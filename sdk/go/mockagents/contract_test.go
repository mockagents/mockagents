package mockagents_test

// Cross-SDK contract runner (Go).
//
// Loads the shared case file sdk/contract/cases.json, executes each case
// through the SDK's public client + assertion API against a running server,
// and checks that every assertion's verdict (pass / fail / error) equals the
// verdict the case file expects. The Python and TypeScript runners read the
// same file, so the three SDKs reach identical verdicts by construction.
//
// Skipped unless MOCKAGENTS_CONTRACT_URL points at a server started with
// `--agents-dir sdk/contract/agents`. With MOCKAGENTS_CONTRACT_TENANT_KEY set
// (the multi-tenant leg) only the cases carrying an `auth` field run; without
// it only the cases that do not. See sdk/contract/README.md.
//
// The Go matchers report failures through testing.TB.Errorf, so each
// assertion runs against a verdictRecorder that captures the failure instead
// of failing the real test; the real test fails only on a verdict mismatch.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mockagents/mockagents/sdk/go/mockagents"
)

const contractSDK = "go"

type contractAssertion struct {
	Kind   string         `json:"kind"`
	Args   map[string]any `json:"args"`
	Expect string         `json:"expect"`
}

type contractCase struct {
	Name            string              `json:"name"`
	Protocol        string              `json:"protocol"`
	Stream          bool                `json:"stream"`
	Model           string              `json:"model"`
	Auth            *string             `json:"auth"`
	Steps           []string            `json:"steps"`
	Assertions      []contractAssertion `json:"assertions"`
	KnownDivergence json.RawMessage     `json:"known_divergence"`
}

type contractSuite struct {
	DefaultModel string         `json:"default_model"`
	WrongAPIKey  string         `json:"wrong_api_key"`
	Cases        []contractCase `json:"cases"`
}

// verdictRecorder is a testing.TB that records matcher failures instead of
// reporting them. The embedded TB satisfies the interface's unexported method;
// every failure path is overridden so nothing reaches the real test.
type verdictRecorder struct {
	testing.TB
	failed bool
	msgs   []string
}

// errFatal unwinds a matcher that called Fatal/FailNow on the recorder.
var errFatal = errors.New("contract: matcher called FailNow")

func (r *verdictRecorder) Helper() {}
func (r *verdictRecorder) Fail()   { r.failed = true }
func (r *verdictRecorder) Failed() bool {
	return r.failed
}
func (r *verdictRecorder) Error(args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprint(args...))
}
func (r *verdictRecorder) Errorf(format string, args ...any) {
	r.failed = true
	r.msgs = append(r.msgs, fmt.Sprintf(format, args...))
}
func (r *verdictRecorder) FailNow() {
	r.failed = true
	panic(errFatal)
}
func (r *verdictRecorder) Fatal(args ...any) {
	r.Error(args...)
	panic(errFatal)
}
func (r *verdictRecorder) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
	panic(errFatal)
}

func loadContractSuite(t *testing.T) *contractSuite {
	t.Helper()
	buf, err := os.ReadFile(filepath.Join("..", "..", "contract", "cases.json"))
	if err != nil {
		t.Fatalf("read cases.json: %v", err)
	}
	var suite contractSuite
	if err := json.Unmarshal(buf, &suite); err != nil {
		t.Fatalf("parse cases.json: %v", err)
	}
	return &suite
}

// divergence returns the known_divergence note that applies to Go, if any.
// The field is "<sdk>: <one line>" or an array of such strings.
func divergence(c contractCase) string {
	if len(c.KnownDivergence) == 0 {
		return ""
	}
	var notes []string
	var one string
	if err := json.Unmarshal(c.KnownDivergence, &one); err == nil {
		notes = []string{one}
	} else {
		_ = json.Unmarshal(c.KnownDivergence, &notes)
	}
	for _, note := range notes {
		sdk, reason, ok := strings.Cut(note, ":")
		if ok && strings.EqualFold(strings.TrimSpace(sdk), contractSDK) {
			return strings.TrimSpace(reason)
		}
	}
	return ""
}

func contractSessionID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return "contract-" + contractSDK + "-" + hex.EncodeToString(b[:])
}

func contractClient(baseURL, tenantKey string, suite *contractSuite, c contractCase) *mockagents.Client {
	opts := mockagents.ClientOptions{BaseURL: baseURL, Timeout: 30 * time.Second}
	if c.Auth != nil {
		switch *c.Auth {
		case "tenant":
			opts.APIKey = tenantKey
		case "wrong":
			opts.APIKey = suite.WrongAPIKey
		}
	}
	return mockagents.NewClient(opts)
}

func runPlain(ctx context.Context, client *mockagents.Client, c contractCase, model string) (*mockagents.ScenarioResult, error) {
	steps := make([]mockagents.ScenarioStep, 0, len(c.Steps))
	for _, s := range c.Steps {
		steps = append(steps, mockagents.ScenarioStep{Role: "user", Content: s})
	}
	sc := mockagents.NewScenario(c.Name, steps)
	sc.Protocol = mockagents.Protocol(c.Protocol)
	sc.Model = model
	sc.SessionID = contractSessionID()
	return mockagents.RunScenario(ctx, client, sc)
}

// runStreamed streams every user step through IterStream and folds each
// turn's chunks into a ChatResponse, so the ordinary matchers can read it.
func runStreamed(ctx context.Context, client *mockagents.Client, c contractCase, model string) (*mockagents.ScenarioResult, error) {
	sid := contractSessionID()
	var history []mockagents.ChatMessage
	result := &mockagents.ScenarioResult{ScenarioName: c.Name}
	for _, step := range c.Steps {
		history = append(history, mockagents.ChatMessage{Role: "user", Content: step})
		stream, err := client.IterStream(ctx, append([]mockagents.ChatMessage(nil), history...), mockagents.IterStreamOptions{
			Protocol:  c.Protocol,
			Model:     model,
			SessionID: sid,
		})
		if err != nil {
			return nil, err
		}
		var text strings.Builder
		finishReason := ""
		finished := false
		for stream.Next() {
			chunk := stream.Value()
			text.WriteString(chunk.Text)
			if chunk.Finished {
				finishReason = chunk.FinishReason
				finished = true
			}
		}
		err = stream.Err()
		_ = stream.Close()
		if err != nil {
			return nil, err
		}
		if !finished {
			return nil, fmt.Errorf("stream for step %q ended without a finished chunk", step)
		}
		resp := &mockagents.ChatResponse{
			Content:      text.String(),
			Model:        model,
			FinishReason: finishReason,
			StatusCode:   200,
		}
		result.Responses = append(result.Responses, resp)
		history = append(history, mockagents.ChatMessage{Role: "assistant", Content: resp.Content})
	}
	return result, nil
}

func argString(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func argInt(args map[string]any, key string) int {
	f, _ := args[key].(float64)
	return int(f)
}

// checkAssertion runs one assertion against rec; it returns an error only for
// a problem with the case itself (unknown kind) or with the non-stream run.
func checkAssertion(rec *verdictRecorder, a contractAssertion, result *mockagents.ScenarioResult, nonstream func() (string, error)) error {
	defer func() {
		if p := recover(); p != nil {
			if p == errFatal {
				return
			}
			panic(p)
		}
	}()
	e := mockagents.ExpectScenario(rec, result)
	args := a.Args
	switch a.Kind {
	case "response_contains":
		e.ToHaveContentContaining(argString(args, "text"))
	case "tool_call":
		var want map[string]any
		if m, ok := args["arguments"].(map[string]any); ok {
			want = m
		}
		e.ToHaveToolCall(argString(args, "name"), want)
	case "tool_call_count":
		if name, ok := args["name"].(string); ok {
			e.ToHaveToolCallCountByName(name, argInt(args, "count"))
		} else {
			e.ToHaveToolCallCount(argInt(args, "count"))
		}
	case "tool_call_sequence":
		raw, _ := args["names"].([]any)
		names := make([]string, 0, len(raw))
		for _, n := range raw {
			s, _ := n.(string)
			names = append(names, s)
		}
		e.ToHaveToolCallSequence(names)
	case "finish_reason":
		e.ToHaveFinishReason(argString(args, "reason"))
	case "status_code":
		e.ToHaveStatusCode(argInt(args, "code"))
	case "stream_text_matches_nonstream":
		plain, err := nonstream()
		if err != nil {
			return fmt.Errorf("non-streamed run: %w", err)
		}
		if streamed := result.LastContent(); streamed != plain {
			rec.Errorf("streamed text %q != non-streamed text %q", streamed, plain)
		}
	default:
		return fmt.Errorf("unknown assertion kind %q", a.Kind)
	}
	return nil
}

func TestContractCases(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("MOCKAGENTS_CONTRACT_URL"))
	if baseURL == "" {
		t.Skip("MOCKAGENTS_CONTRACT_URL is not set (cross-SDK contract suite)")
	}
	tenantKey := strings.TrimSpace(os.Getenv("MOCKAGENTS_CONTRACT_TENANT_KEY"))
	multiTenant := tenantKey != ""
	suite := loadContractSuite(t)

	for _, c := range suite.Cases {
		t.Run(c.Name, func(t *testing.T) {
			if (c.Auth != nil) != multiTenant {
				t.Skip("case belongs to the other leg (set/unset MOCKAGENTS_CONTRACT_TENANT_KEY)")
			}
			if note := divergence(c); note != "" {
				t.Skip("known divergence: " + note)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			model := c.Model
			if model == "" {
				model = suite.DefaultModel
			}
			client := contractClient(baseURL, tenantKey, suite, c)

			var (
				result *mockagents.ScenarioResult
				runErr error
			)
			if c.Stream {
				result, runErr = runStreamed(ctx, client, c, model)
			} else {
				result, runErr = runPlain(ctx, client, c, model)
			}

			var plainText *string
			nonstream := func() (string, error) {
				if plainText == nil {
					res, err := runPlain(ctx, contractClient(baseURL, tenantKey, suite, c), c, model)
					if err != nil {
						return "", err
					}
					s := res.LastContent()
					plainText = &s
				}
				return *plainText, nil
			}

			var mismatches []string
			for i, a := range c.Assertions {
				verdict, detail := "", ""
				if runErr != nil {
					// The SDK returned an error: every verdict is "error".
					verdict, detail = "error", runErr.Error()
				} else {
					rec := &verdictRecorder{TB: t}
					if err := checkAssertion(rec, a, result, nonstream); err != nil {
						t.Fatalf("#%d %s: %v", i, a.Kind, err)
					}
					verdict = "pass"
					if rec.failed {
						verdict, detail = "fail", strings.Join(rec.msgs, "; ")
					}
				}
				if verdict != a.Expect {
					args, _ := json.Marshal(a.Args)
					mismatches = append(mismatches, fmt.Sprintf("#%d %s %s: expected %s, got %s (%s)",
						i, a.Kind, args, a.Expect, verdict, detail))
				}
			}
			if len(mismatches) > 0 {
				t.Errorf("%s [%s]:\n  %s", c.Name, contractSDK, strings.Join(mismatches, "\n  "))
			}
		})
	}
}
