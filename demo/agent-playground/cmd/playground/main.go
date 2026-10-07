// Command playground is the mockagents Agent Playground: a multi-agent demo
// application (server, web UI and CLI) that runs entirely against mockagents.
//
//	go run ./demo/agent-playground/cmd/playground serve
//	open http://127.0.0.1:7070
//
// See demo/agent-playground/README.md.
package main

import (
	"os"

	"github.com/mockagents/mockagents/demo/agent-playground/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
