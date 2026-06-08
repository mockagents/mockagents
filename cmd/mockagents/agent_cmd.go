package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// Exit codes for the agent write commands:
//   0 - the server accepted the write
//   2 - any failure: a server rejection (validation/conflict/not found), an
//       unreachable server, or a local error. rootCmd maps every non-nil RunE
//       error to exit 2; the error message distinguishes the cause.

var (
	agentServerURL string
	agentAPIKey    string
	agentReplace   bool
)

var addCmd = &cobra.Command{
	Use:   "add <file>",
	Short: "Create an agent on a running server from a YAML/JSON file",
	Long: `Send an agent definition file to a running MockAgents server's write API so
the agent is registered immediately, with no restart.

By default this creates the agent (POST /api/v1/agents) and fails if one of the
same name already exists. Pass --replace to upsert it instead (PUT
/api/v1/agents/{name}).

The server URL defaults to $MOCKAGENTS_SERVER or http://localhost:8080. When the
server runs in multi-tenant mode, supply an editor-or-higher API key with
--api-key or $MOCKAGENTS_API_KEY.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runAdd,
	SilenceUsage:  true, // a server rejection isn't a usage error
	SilenceErrors: true, // main() prints the error once
}

var rmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Delete an agent from a running server",
	Long: `Delete an agent by name from a running MockAgents server (DELETE
/api/v1/agents/{name}). The agent stops serving immediately and its persisted
file (if any) is removed.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runRm,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	for _, c := range []*cobra.Command{addCmd, rmCmd} {
		c.Flags().StringVar(&agentServerURL, "server", envOrDefault("MOCKAGENTS_SERVER", "http://localhost:8080"), "Base URL of the running MockAgents server")
		c.Flags().StringVar(&agentAPIKey, "api-key", os.Getenv("MOCKAGENTS_API_KEY"), "API key (editor+) for multi-tenant servers")
	}
	addCmd.Flags().BoolVar(&agentReplace, "replace", false, "Replace an existing agent (PUT upsert) instead of failing on conflict")

	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(rmCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	file := args[0]
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	contentType := "application/yaml"
	if strings.EqualFold(filepath.Ext(file), ".json") {
		contentType = "application/json"
	}

	base := strings.TrimRight(agentServerURL, "/")
	method := http.MethodPost
	reqURL := base + "/api/v1/agents"
	if agentReplace {
		name, nerr := agentNameFromBytes(data)
		if nerr != nil {
			return fmt.Errorf("--replace needs the file's metadata.name: %w", nerr)
		}
		method = http.MethodPut
		reqURL = base + "/api/v1/agents/" + url.PathEscape(name)
	}

	code, body, err := mgmtRequest(method, reqURL, agentAPIKey, contentType, data)
	if err != nil {
		return unreachableErr(base, err)
	}
	if code >= 200 && code < 300 {
		printSuccess(fmt.Sprintf("Agent accepted (%s %d)", method, code))
		fmt.Println(strings.TrimSpace(string(body)))
		return nil
	}
	return serverRejectedErr(code, body)
}

func runRm(cmd *cobra.Command, args []string) error {
	name := args[0]
	base := strings.TrimRight(agentServerURL, "/")
	reqURL := base + "/api/v1/agents/" + url.PathEscape(name)

	code, body, err := mgmtRequest(http.MethodDelete, reqURL, agentAPIKey, "", nil)
	if err != nil {
		return unreachableErr(base, err)
	}
	if code >= 200 && code < 300 {
		printSuccess(fmt.Sprintf("Agent %q deleted", name))
		fmt.Println(strings.TrimSpace(string(body)))
		return nil
	}
	return serverRejectedErr(code, body)
}

// mgmtRequest performs a single management-API request and returns the status
// code and response body. A transport error (server down, DNS, timeout) is
// returned as err; an HTTP error status is returned via code with err nil so
// callers can surface the server's message.
func mgmtRequest(method, url, apiKey, contentType string, body []byte) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, respBody, nil
}

// agentNameFromBytes extracts metadata.name from a YAML or JSON agent document
// (yaml.v3 parses JSON too). Used to build the PUT path for --replace.
func agentNameFromBytes(data []byte) (string, error) {
	var doc struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parsing document: %w", err)
	}
	if strings.TrimSpace(doc.Metadata.Name) == "" {
		return "", fmt.Errorf("metadata.name is empty")
	}
	return doc.Metadata.Name, nil
}

func unreachableErr(base string, err error) error {
	return fmt.Errorf("could not reach the server at %s (%v) — is `mockagents start` running?", base, err)
}

// serverRejectedErr turns a non-2xx response into a CLI error carrying the
// server's message. The commands set SilenceUsage/SilenceErrors so this surfaces
// as a single clean line (printed by main) rather than a usage dump.
func serverRejectedErr(code int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(code)
	}
	return fmt.Errorf("server rejected the request (HTTP %d): %s", code, msg)
}
