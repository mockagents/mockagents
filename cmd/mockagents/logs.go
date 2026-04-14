package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/mockagents/mockagents/internal/storage"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Query interaction logs",
	Long: `Display logged mock server interactions. Logs are stored in a local
SQLite database and include request/response bodies, latency, agent name,
and scenario information.`,
	RunE: runLogs,
}

var (
	logsAgent     string
	logsSession   string
	logsSince     string
	logsLimit     int
	logsOutputFmt string
	logsDBPath    string
)

func init() {
	logsCmd.Flags().StringVar(&logsAgent, "agent", "", "Filter by agent name")
	logsCmd.Flags().StringVar(&logsSession, "session", "", "Filter by session ID")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Show logs since duration (e.g., 1h, 30m)")
	logsCmd.Flags().IntVar(&logsLimit, "limit", 50, "Maximum number of results")
	logsCmd.Flags().StringVar(&logsOutputFmt, "output", "table", "Output format: table or json")
	logsCmd.Flags().StringVar(&logsDBPath, "db", ".mockagents.db", "Path to SQLite database")
}

func runLogs(cmd *cobra.Command, args []string) error {
	store, err := storage.NewSQLiteStore(logsDBPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()

	filter := storage.InteractionFilter{
		AgentName: logsAgent,
		SessionID: logsSession,
		Limit:     logsLimit,
	}

	if logsSince != "" {
		dur, err := time.ParseDuration(logsSince)
		if err != nil {
			return fmt.Errorf("invalid --since value %q: %w", logsSince, err)
		}
		filter.Since = time.Now().Add(-dur).UTC().Format(time.RFC3339)
	}

	logs, err := store.Query(cmd.Context(), filter)
	if err != nil {
		return fmt.Errorf("querying logs: %w", err)
	}

	if logsOutputFmt == "json" {
		return printLogsJSON(logs)
	}
	return printLogsTable(logs)
}

func printLogsTable(logs []storage.InteractionLog) error {
	if len(logs) == 0 {
		fmt.Println("No interaction logs found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIMESTAMP\tAGENT\tPROTOCOL\tSTATUS\tLATENCY\tSCENARIO")
	fmt.Fprintln(w, "--\t---------\t-----\t--------\t------\t-------\t--------")

	for _, log := range logs {
		ts := log.Timestamp
		if len(ts) > 19 {
			ts = ts[:19]
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%dms\t%s\n",
			log.ID, ts, log.AgentName, log.Protocol,
			log.ResponseStatus, log.LatencyMs, log.ScenarioName,
		)
	}
	return w.Flush()
}

func printLogsJSON(logs []storage.InteractionLog) error {
	if logs == nil {
		logs = []storage.InteractionLog{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(logs)
}
