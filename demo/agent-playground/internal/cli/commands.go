package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/app"
)

const usage = `playground: the mockagents Agent Playground

Server:
  playground serve [--addr 127.0.0.1:7070] [--mock embedded|URL] [--review-mode manual|auto]
                   [--config playground.json] [--fixtures DIR] [--state-file runs.json] [--token T]

Client (talks to --server, default $PLAYGROUND_URL or http://127.0.0.1:7070):
  playground workflows                          list workflows and their examples
  playground run <workflow> [--input JSON] [--example NAME] [--review-mode auto] [--wait 60] [--watch]
  playground runs [--status S] [--workflow W]   list runs
  playground show <run-id>                      run summary, steps and output
  playground trace <run-id>                     span tree (agents, attempts, retries, tools, reviews)
  playground cancel <run-id> [--reason R]
  playground reviews [--all]                    pending human-review checkpoints
  playground approve|reject|revise <review-id> [--comment C] [--reviewer NAME]
  playground agents                             agent catalog with effective routing
  playground configure <agent> key=value ...    merge-patch an agent (e.g. tier=llm retry.max_retries=5)
  playground invoke <agent> <message...> [--stream] [--tier slm|llm]
  playground config [show|set key=value ...|reset]
  playground mock [status|logs|costs|pipeline NAME INPUT]
  playground verify [--embedded]                end-to-end self-check of every feature
  playground version

Global flags: --server URL, --token TOKEN, --json (raw JSON output)
`

// Main runs the CLI and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "serve":
		err = serve(rest, stderr)
	case "verify":
		err = verifyCmd(rest, stdout)
	case "version":
		fmt.Fprintln(stdout, "playground 1.0.0")
	default:
		err = clientCmd(cmd, rest, stdout)
	}
	if err != nil {
		var ue usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(stderr, "error: %v\n\n%s", err, usage)
			return 2
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

// parseInterspersed lets flags appear after positional arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		f := fs.Lookup(name)
		if f == nil {
			continue // let fs.Parse report it
		}
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	if err := fs.Parse(flags); err != nil {
		return nil, usageError{err.Error()}
	}
	return pos, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func serve(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", envOr("PLAYGROUND_ADDR", "127.0.0.1:7070"), "listen address")
	mock := fs.String("mock", envOr("PLAYGROUND_MOCK_URL", "embedded"), "\"embedded\" or an external mockagents URL")
	mockKey := fs.String("mock-key", os.Getenv("PLAYGROUND_MOCK_API_KEY"), "API key for the mock (provider + management API)")
	mockAddr := fs.String("mock-addr", envOr("PLAYGROUND_MOCK_ADDR", "127.0.0.1:0"), "embedded mock listen address")
	cfgPath := fs.String("config", os.Getenv("PLAYGROUND_CONFIG"), "playground.json (default: embedded)")
	fixtures := fs.String("fixtures", os.Getenv("PLAYGROUND_FIXTURES"), "mockagents fixtures dir for the embedded mock (default: embedded)")
	state := fs.String("state-file", os.Getenv("PLAYGROUND_STATE_FILE"), "persist runs/reviews to this JSON file")
	review := fs.String("review-mode", os.Getenv("PLAYGROUND_REVIEW_MODE"), "override review mode: manual | auto")
	token := fs.String("token", os.Getenv("PLAYGROUND_TOKEN"), "require this bearer token on mutating API calls")
	level := fs.String("log-level", envOr("PLAYGROUND_LOG_LEVEL", "info"), "debug | info | warn | error")
	if _, err := parseInterspersed(fs, args); err != nil {
		return err
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(*level)); err != nil {
		return usageError{"invalid --log-level"}
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: lvl}))
	a, err := app.New(app.Options{
		Addr: *addr, ConfigPath: *cfgPath, MockURL: *mock, MockAPIKey: *mockKey, MockAddr: *mockAddr,
		FixturesDir: *fixtures, StatePath: *state, ReviewMode: *review, Token: *token, Logger: logger,
	})
	if err != nil {
		return err
	}
	defer a.Close()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	logger.Info("Agent Playground ready",
		"ui", "http://"+ln.Addr().String()+"/",
		"api", "http://"+ln.Addr().String()+"/api/info",
		"openapi", "http://"+ln.Addr().String()+"/openapi.yaml",
		"mock", a.MockURL, "mock_mode", a.MockMode,
		"review_mode", a.Config.Current().Defaults.Review.Mode)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx, ln)
}

type globals struct {
	server, token string
	asJSON        bool
}

func addGlobals(fs *flag.FlagSet) *globals {
	g := &globals{}
	fs.StringVar(&g.server, "server", envOr("PLAYGROUND_URL", "http://127.0.0.1:7070"), "playground URL")
	fs.StringVar(&g.token, "token", os.Getenv("PLAYGROUND_TOKEN"), "bearer token")
	fs.BoolVar(&g.asJSON, "json", false, "print raw JSON")
	return g
}

func printJSON(w io.Writer, v any) {
	raw, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(w, string(raw))
}

func clientCmd(cmd string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g := addGlobals(fs)
	input := fs.String("input", "", "workflow input JSON")
	example := fs.String("example", "", "use a workflow example by name")
	reviewMode := fs.String("review-mode", "", "manual | auto")
	wait := fs.Int("wait", 60, "seconds to wait for the run to settle")
	watch := fs.Bool("watch", false, "follow the run until it finishes")
	status := fs.String("status", "", "filter by status")
	wfFilter := fs.String("workflow", "", "filter by workflow")
	reason := fs.String("reason", "", "cancel reason")
	comment := fs.String("comment", "", "review comment")
	reviewer := fs.String("reviewer", envOr("USER", envOr("USERNAME", "cli")), "reviewer name")
	all := fs.Bool("all", false, "include non-pending/non-blocking reviews")
	stream := fs.Bool("stream", false, "stream the agent's output")
	tier := fs.String("tier", "", "slm | llm")
	limit := fs.Int("limit", 20, "max rows")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	c := NewClient(g.server, g.token)
	ctx := context.Background()
	need := func(n int, what string) error {
		if len(pos) < n {
			return usageError{fmt.Sprintf("%s requires %s", cmd, what)}
		}
		return nil
	}

	switch cmd {
	case "workflows":
		var res struct {
			Workflows []struct {
				Name     string   `json:"name"`
				Summary  string   `json:"summary"`
				Patterns []string `json:"patterns"`
				Examples []struct {
					Name        string         `json:"name"`
					Description string         `json:"description"`
					Input       map[string]any `json:"input"`
				} `json:"examples"`
			} `json:"workflows"`
		}
		if err := c.Get(ctx, "/api/workflows", nil, &res); err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, res)
			return nil
		}
		for _, w := range res.Workflows {
			fmt.Fprintf(out, "%s\n  %s\n  patterns: %s\n", w.Name, w.Summary, strings.Join(w.Patterns, ", "))
			for _, e := range w.Examples {
				raw, _ := json.Marshal(e.Input)
				fmt.Fprintf(out, "    --example %-22s %s  %s\n", e.Name, e.Description, raw)
			}
			fmt.Fprintln(out)
		}
		return nil

	case "run":
		if err := need(1, "a workflow name"); err != nil {
			return err
		}
		in := map[string]any{}
		switch {
		case *input != "":
			if err := json.Unmarshal([]byte(*input), &in); err != nil {
				return usageError{"--input must be a JSON object: " + err.Error()}
			}
		case *example != "":
			var wf struct {
				Examples []struct {
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"examples"`
			}
			if err := c.Get(ctx, "/api/workflows/"+url.PathEscape(pos[0]), nil, &wf); err != nil {
				return err
			}
			found := false
			for _, e := range wf.Examples {
				if e.Name == *example {
					in, found = e.Input, true
				}
			}
			if !found {
				return usageError{fmt.Sprintf("workflow %s has no example %q", pos[0], *example)}
			}
		}
		var opts map[string]any
		if *reviewMode != "" {
			opts = map[string]any{"review_mode": *reviewMode}
		}
		run, err := c.StartRun(ctx, pos[0], in, opts, time.Duration(*wait)*time.Second)
		if err != nil {
			return err
		}
		if *watch && !run.Terminal() {
			run, err = c.WaitRun(ctx, run.ID, nil, 30*time.Minute)
			if err != nil {
				return err
			}
		}
		if g.asJSON {
			printJSON(out, run)
			return nil
		}
		printRun(out, run)
		if run.Waiting() {
			reviews, _ := c.PendingReviews(ctx, run.ID)
			for _, r := range reviews {
				fmt.Fprintf(out, "\n>> waiting for review %s (%s): %s\n   allowed: %s\n   e.g. playground approve %s\n",
					r.ID, r.Kind, r.Title, strings.Join(r.AllowedActions, ", "), r.ID)
			}
		}
		return nil

	case "runs":
		q := url.Values{"limit": {strconv.Itoa(*limit)}}
		if *status != "" {
			q.Set("status", *status)
		}
		if *wfFilter != "" {
			q.Set("workflow", *wfFilter)
		}
		var res struct {
			Runs   []Run          `json:"runs"`
			Counts map[string]int `json:"counts"`
		}
		if err := c.Get(ctx, "/api/runs", q, &res); err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, res)
			return nil
		}
		fmt.Fprintf(out, "%-26s %-18s %-12s %-18s %8s %6s %s\n", "RUN", "WORKFLOW", "STATUS", "PHASE", "MS", "RETRY", "ERROR")
		for _, r := range res.Runs {
			errCode := ""
			if r.Error != nil {
				errCode = r.Error.Code
			}
			fmt.Fprintf(out, "%-26s %-18s %-12s %-18s %8d %6d %s\n", r.ID, r.Workflow, r.Status, r.Phase, r.DurationMS, r.Stats.Retries, errCode)
		}
		fmt.Fprintf(out, "\nin_progress=%d completed=%d failed=%d\n", res.Counts["in_progress"], res.Counts["completed"], res.Counts["failed"])
		return nil

	case "show":
		if err := need(1, "a run id"); err != nil {
			return err
		}
		run, err := c.GetRun(ctx, pos[0])
		if err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, run)
			return nil
		}
		printRun(out, run)
		return nil

	case "trace":
		if err := need(1, "a run id"); err != nil {
			return err
		}
		var res struct {
			Spans []Span `json:"spans"`
		}
		if err := c.Get(ctx, "/api/runs/"+url.PathEscape(pos[0])+"/trace", nil, &res); err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, res)
			return nil
		}
		PrintTrace(out, res.Spans)
		return nil

	case "cancel":
		if err := need(1, "a run id"); err != nil {
			return err
		}
		var run Run
		if _, err := c.Post(ctx, "/api/runs/"+url.PathEscape(pos[0])+"/cancel", nil, map[string]any{"reason": *reason}, &run); err != nil {
			return err
		}
		fmt.Fprintf(out, "%s is now %s (%s)\n", run.ID, run.Status, run.Error.Code)
		return nil

	case "reviews":
		q := url.Values{}
		if !*all {
			q.Set("status", "pending")
			q.Set("blocking", "true")
		}
		var res struct {
			Reviews []Review `json:"reviews"`
		}
		if err := c.Get(ctx, "/api/reviews", q, &res); err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, res)
			return nil
		}
		if len(res.Reviews) == 0 {
			fmt.Fprintln(out, "no reviews")
		}
		for _, r := range res.Reviews {
			fmt.Fprintf(out, "%s  [%s/%s] run=%s  %s\n    %s\n    actions: %s\n\n", r.ID, r.Kind, r.Status, r.RunID, r.Title,
				truncate(strings.ReplaceAll(r.Content, "\n", " "), 220), strings.Join(r.AllowedActions, ", "))
		}
		return nil

	case "approve", "reject", "revise", "flag":
		if err := need(1, "a review id"); err != nil {
			return err
		}
		r, err := c.Decide(ctx, pos[0], cmd, *comment, *reviewer)
		if err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, r)
			return nil
		}
		fmt.Fprintf(out, "%s -> %s (run %s)\n", r.ID, r.Status, r.RunID)
		return nil

	case "agents":
		var res struct {
			Agents []struct {
				Name           string   `json:"name"`
				Role           string   `json:"role"`
				Tier           string   `json:"tier"`
				Tools          []string `json:"tools"`
				EffectiveRoute struct {
					Tier  string `json:"tier"`
					Model struct {
						Provider string `json:"provider"`
						Model    string `json:"model"`
					} `json:"model"`
				} `json:"effective_route"`
				Fallback []struct {
					Model string `json:"model"`
				} `json:"fallback"`
			} `json:"agents"`
		}
		if err := c.Get(ctx, "/api/agents", nil, &res); err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, res)
			return nil
		}
		fmt.Fprintf(out, "%-20s %-10s %-5s %-4s %-30s %s\n", "AGENT", "ROLE", "TIER", "->", "MODEL", "TOOLS")
		for _, a := range res.Agents {
			fmt.Fprintf(out, "%-20s %-10s %-5s %-4s %-30s %s\n", a.Name, a.Role, a.Tier, a.EffectiveRoute.Tier,
				a.EffectiveRoute.Model.Provider+"/"+a.EffectiveRoute.Model.Model, strings.Join(a.Tools, ","))
		}
		return nil

	case "configure":
		if err := need(2, "an agent and key=value pairs"); err != nil {
			return err
		}
		patch, err := kvPatch(pos[1:])
		if err != nil {
			return err
		}
		var res any
		if _, err := c.Do(ctx, "PATCH", "/api/agents/"+url.PathEscape(pos[0]), nil, patch, &res); err != nil {
			return describeErr(err)
		}
		printJSON(out, res)
		return nil

	case "invoke":
		if err := need(2, "an agent and a message"); err != nil {
			return err
		}
		body := map[string]any{"input": strings.Join(pos[1:], " ")}
		if *tier != "" {
			body["tier"] = *tier
		}
		if *stream {
			body["stream"] = true
			return c.Stream(ctx, "/api/agents/"+url.PathEscape(pos[0])+"/invoke", body, func(event string, data json.RawMessage) bool {
				var m map[string]any
				_ = json.Unmarshal(data, &m)
				switch event {
				case "delta":
					fmt.Fprint(out, m["text"])
				case "reset":
					fmt.Fprintf(out, "\n[stream broken: %v; retrying without streaming]\n", m["reason"])
				case "retry":
					fmt.Fprintf(out, "\n[retry after attempt %v: %v, waiting %vms]\n", m["attempt"], m["reason"], m["delay_ms"])
				case "fallback":
					fmt.Fprintf(out, "\n[fallback %v -> %v]\n", m["from"], m["to"])
				case "tool_call":
					fmt.Fprintf(out, "\n[tool %v(%v)]\n", m["tool"], m["arguments"])
				case "route":
					fmt.Fprintf(out, "[route: %v -> %v (%v)]\n", m["agent"], m["model"], m["reason"])
				case "done":
					fmt.Fprintf(out, "\n\n[done: status=%v run=%v]\n", m["status"], m["run_id"])
					return false
				}
				return true
			})
		}
		var res map[string]any
		if _, err := c.Post(ctx, "/api/agents/"+url.PathEscape(pos[0])+"/invoke", nil, body, &res); err != nil {
			return err
		}
		if g.asJSON {
			printJSON(out, res)
			return nil
		}
		if r, ok := res["result"].(map[string]any); ok {
			fmt.Fprintf(out, "%v\n\n[%v via %v | attempts=%v retries=%v fallback=%v | $%.6f | run %v]\n",
				r["output"], r["tier"], modelStr(r["model"]), r["attempts"], r["retries"], r["fallback_used"], num(r["cost_usd"]), res["run_id"])
		} else {
			printJSON(out, res)
		}
		return nil

	case "config":
		sub := "show"
		if len(pos) > 0 {
			sub = pos[0]
		}
		switch sub {
		case "show":
			var res any
			if err := c.Get(ctx, "/api/config", nil, &res); err != nil {
				return err
			}
			printJSON(out, res)
		case "set":
			patch, err := kvPatch(pos[1:])
			if err != nil {
				return err
			}
			var res map[string]any
			if _, err := c.Do(ctx, "PATCH", "/api/config", nil, patch, &res); err != nil {
				return describeErr(err)
			}
			fmt.Fprintf(out, "config updated (version %v)\n", res["version"])
		case "reset":
			var res map[string]any
			if _, err := c.Post(ctx, "/api/config/reset", nil, nil, &res); err != nil {
				return err
			}
			fmt.Fprintf(out, "config reset (version %v)\n", res["version"])
		default:
			return usageError{"config subcommands: show | set key=value ... | reset"}
		}
		return nil

	case "mock":
		sub := "status"
		if len(pos) > 0 {
			sub = pos[0]
		}
		var res any
		switch sub {
		case "status":
			err = c.Get(ctx, "/api/mock/status", nil, &res)
		case "logs":
			err = c.Get(ctx, "/api/mock/logs", url.Values{"limit": {strconv.Itoa(*limit)}}, &res)
		case "costs":
			err = c.Get(ctx, "/api/mock/costs", nil, &res)
		case "pipeline":
			if len(pos) < 3 {
				return usageError{"mock pipeline NAME INPUT"}
			}
			_, err = c.Post(ctx, "/api/mock/pipelines/"+url.PathEscape(pos[1])+"/run", nil, map[string]any{"input": strings.Join(pos[2:], " ")}, &res)
		default:
			return usageError{"mock subcommands: status | logs | costs | pipeline NAME INPUT"}
		}
		if err != nil {
			return err
		}
		printJSON(out, res)
		return nil
	}
	return usageError{fmt.Sprintf("unknown command %q", cmd)}
}

// kvPatch turns key=value pairs into a nested merge patch. Values are parsed
// as JSON when possible (numbers, booleans, null, objects), else strings.
func kvPatch(pairs []string) (map[string]any, error) {
	patch := map[string]any{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, usageError{fmt.Sprintf("expected key=value, got %q", p)}
		}
		var val any
		if err := json.Unmarshal([]byte(v), &val); err != nil {
			val = v
		}
		parts := strings.Split(k, ".")
		m := patch
		for _, part := range parts[:len(parts)-1] {
			next, ok := m[part].(map[string]any)
			if !ok {
				next = map[string]any{}
				m[part] = next
			}
			m = next
		}
		m[parts[len(parts)-1]] = val
	}
	return patch, nil
}

func describeErr(err error) error {
	var ae *APIError
	if errors.As(err, &ae) && ae.Details != nil {
		raw, _ := json.MarshalIndent(ae.Details, "  ", "  ")
		return fmt.Errorf("%s\n  %s", ae.Error(), raw)
	}
	return err
}

func printRun(out io.Writer, r *Run) {
	fmt.Fprintf(out, "run %s  workflow=%s  status=%s  phase=%s  %dms\n", r.ID, r.Workflow, r.Status, r.Phase, r.DurationMS)
	fmt.Fprintf(out, "stats: llm_calls=%d (slm=%d llm=%d) attempts=%d retries=%d fallbacks=%d escalations=%d tools=%d cost=$%.6f\n",
		r.Stats.LLMCalls, r.Stats.SLMCalls, r.Stats.LLMTierCall, r.Stats.Attempts, r.Stats.Retries, r.Stats.Fallbacks, r.Stats.Escalations, r.Stats.ToolCalls, r.Stats.CostUSD)
	for _, s := range r.Steps {
		mark := map[string]string{"completed": "OK  ", "failed": "FAIL", "running": "... ", "skipped": "SKIP"}[s.Status]
		extra := ""
		if s.Model != "" {
			extra = fmt.Sprintf(" [%s %s]", s.Tier, s.Model)
		}
		if s.Retries > 0 {
			extra += fmt.Sprintf(" retries=%d", s.Retries)
		}
		if s.Fallback {
			extra += " fallback"
		}
		if s.Escalated {
			extra += " escalated"
		}
		fmt.Fprintf(out, "  %s %-44s %6dms%s\n", mark, s.Name, s.DurationMS, extra)
		for _, n := range s.Notes {
			fmt.Fprintf(out, "         - %s\n", n)
		}
		if s.Error != "" {
			fmt.Fprintf(out, "         ! %s\n", truncate(s.Error, 200))
		}
	}
	if r.Error != nil {
		fmt.Fprintf(out, "error: %s: %s\n", r.Error.Code, r.Error.Message)
	}
	if r.Output != nil {
		raw, _ := json.MarshalIndent(r.Output, "", "  ")
		fmt.Fprintf(out, "output:\n%s\n", truncate(string(raw), 3000))
	}
}

// Span is a trace span (subset).
type Span struct {
	ID       string         `json:"id"`
	ParentID string         `json:"parent_id"`
	Name     string         `json:"name"`
	Kind     string         `json:"kind"`
	Start    time.Time      `json:"start"`
	End      *time.Time     `json:"end"`
	Status   string         `json:"status"`
	Error    string         `json:"error"`
	Attrs    map[string]any `json:"attrs"`
	Events   []struct {
		Name  string         `json:"name"`
		Attrs map[string]any `json:"attrs"`
	} `json:"events"`
}

// PrintTrace renders spans as an indented tree.
func PrintTrace(out io.Writer, spans []Span) {
	children := map[string][]Span{}
	for _, s := range spans {
		children[s.ParentID] = append(children[s.ParentID], s)
	}
	var t0 time.Time
	if len(spans) > 0 {
		t0 = spans[0].Start
	}
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		kids := children[parent]
		sort.SliceStable(kids, func(i, j int) bool { return kids[i].Start.Before(kids[j].Start) })
		for _, s := range kids {
			dur := "running"
			if s.End != nil {
				dur = fmt.Sprintf("%dms", s.End.Sub(s.Start).Milliseconds())
			}
			mark := map[string]string{"ok": "+", "error": "x", "running": "~"}[s.Status]
			attrs := spanAttrs(s)
			fmt.Fprintf(out, "%s%s %-40s %8s  @%-6d %s\n", strings.Repeat("  ", depth), mark, s.Name, dur, s.Start.Sub(t0).Milliseconds(), attrs)
			for _, ev := range s.Events {
				fmt.Fprintf(out, "%s    * %s %s\n", strings.Repeat("  ", depth), ev.Name, compact(ev.Attrs))
			}
			if s.Error != "" && s.Kind != "workflow" {
				fmt.Fprintf(out, "%s    ! %s\n", strings.Repeat("  ", depth), truncate(s.Error, 160))
			}
			walk(s.ID, depth+1)
		}
	}
	walk("", 0)
}

func spanAttrs(s Span) string {
	keys := []string{"tier", "model", "reason", "http_status", "error_class", "retry_after_ms", "finish_reason", "tool_calls", "outcome", "approved", "action", "reviewer", "fallback", "attempts", "cost_usd"}
	var parts []string
	for _, k := range keys {
		if v, ok := s.Attrs[k]; ok {
			if k == "fallback" && v == false {
				continue
			}
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return strings.Join(parts, " ")
}

func compact(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return truncate(strings.Join(parts, " "), 200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func modelStr(v any) string {
	if m, ok := v.(map[string]any); ok {
		return fmt.Sprintf("%v/%v", m["provider"], m["model"])
	}
	return fmt.Sprint(v)
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
