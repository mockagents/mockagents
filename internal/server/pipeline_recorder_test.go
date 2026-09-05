package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/storage"
)

// A pipeline run used to leave no trace at all: InteractionCapture is HTTP
// middleware and the executor calls the engine in process, so three agents
// could execute while the log showed nothing. These pin both halves of the fix
// — that the rows appear, and that they do not pretend to be HTTP requests.
func recorderFixture(t *testing.T, mode LogBodyMode) (*pipelineRecorder, *storage.SQLiteStore) {
	t.Helper()
	store, err := storage.NewSQLiteStore(t.TempDir() + "/logs.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	worker := NewLogWorker(store, nil, LogWorkerConfig{Workers: 1, QueueSize: 16})
	t.Cleanup(func() { worker.Shutdown(time.Second) })
	rec := newPipelineRecorder(worker, mode)
	if rec == nil {
		t.Fatal("recorder should exist when a worker does")
	}
	return rec.(*pipelineRecorder), store
}

func drain(t *testing.T, store *storage.SQLiteStore) []storage.InteractionLog {
	t.Helper()
	// The worker writes asynchronously; poll rather than sleep a fixed amount.
	for range 100 {
		rows, err := store.Query(context.Background(), storage.InteractionFilter{})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) > 0 {
			return rows
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no rows were written")
	return nil
}

func TestPipelineRecorder_RecordsANodeAsANonHTTPInteraction(t *testing.T) {
	rec, store := recorderFixture(t, LogBodyFull)
	rec.RecordNode(context.Background(), engine.NodeInteraction{
		PipelineName: "support-triage",
		NodeID:       "router",
		AgentName:    "router-agent",
		SessionID:    "run-1::support-triage::router",
		Protocol:     "anthropic-messages",
		Input:        "where is my order",
		Response:     &engine.Response{Content: "checking", ScenarioName: "order-status"},
		Latency:      12 * time.Millisecond,
	})

	rows := drain(t, store)
	row := rows[0]

	if row.Source != storage.SourcePipeline {
		t.Errorf("source = %q, want %q", row.Source, storage.SourcePipeline)
	}
	// The session is the SCOPED one, which is what makes a run findable by
	// prefix from the screen that started it.
	if row.SessionID != "run-1::support-triage::router" {
		t.Errorf("session = %q, want the scoped session", row.SessionID)
	}
	if row.AgentName != "router-agent" || row.ScenarioName != "order-status" {
		t.Errorf("row lost its identity: %+v", row)
	}

	// The part that matters as much as recording it at all: this was not an
	// HTTP request, and the row must not claim otherwise. A fabricated 200 and
	// a plausible path would make a node indistinguishable from real traffic.
	if row.ResponseStatus != 0 {
		t.Errorf("response_status = %d, want 0 — there was no HTTP response", row.ResponseStatus)
	}
	if row.RequestMethod != "" || row.RequestPath != "" {
		t.Errorf("method/path = %q/%q, want empty", row.RequestMethod, row.RequestPath)
	}
}

func TestPipelineRecorder_RecordsAFailedNode(t *testing.T) {
	rec, store := recorderFixture(t, LogBodyFull)
	rec.RecordNode(context.Background(), engine.NodeInteraction{
		PipelineName: "support-triage",
		NodeID:       "lookup",
		AgentName:    "missing-agent",
		SessionID:    "run-2::support-triage::lookup",
		Input:        "anything",
		Err:          engine.ErrAgentNotFound,
	})

	row := drain(t, store)[0]
	if row.Error == "" {
		t.Error("a node that could not run is exactly the interaction someone goes to the log to find")
	}
	// Named even though it does not exist: "which agent was missing" is the
	// useful thing about this failure.
	if row.AgentName != "missing-agent" {
		t.Errorf("agent = %q, want the ref that could not be resolved", row.AgentName)
	}
}

func TestPipelineRecorder_HonoursTheBodyPolicy(t *testing.T) {
	t.Run("none records no bodies at all", func(t *testing.T) {
		rec, store := recorderFixture(t, LogBodyNone)
		rec.RecordNode(context.Background(), engine.NodeInteraction{
			NodeID: "n", AgentName: "a", SessionID: "s",
			Input:    "my api key is sk-secret",
			Response: &engine.Response{Content: "ok"},
		})
		row := drain(t, store)[0]
		// A run must not be a loophole around a deployment's capture settings.
		if row.RequestBody != "" || row.ResponseBody != "" {
			t.Errorf("bodies recorded under LogBodyNone: %+v", row)
		}
	})

	t.Run("full records the node input in a readable envelope", func(t *testing.T) {
		rec, store := recorderFixture(t, LogBodyFull)
		rec.RecordNode(context.Background(), engine.NodeInteraction{
			PipelineName: "p", NodeID: "n", AgentName: "a", SessionID: "s",
			Input:    "where is my order",
			Response: &engine.Response{Content: "checking"},
		})
		row := drain(t, store)[0]

		var got pipelineNodeInput
		if err := json.Unmarshal([]byte(row.RequestBody), &got); err != nil {
			t.Fatalf("request body should be JSON like every other row: %v", err)
		}
		if got.Input != "where is my order" || got.Pipeline != "p" || got.Node != "n" {
			t.Errorf("envelope = %+v", got)
		}
		if row.ResponseBody == "" {
			t.Error("the node's response is the other half of the evidence")
		}
	})
}

// Nil rather than a null object, so the executor's own nil check does the right
// thing for an embedding caller with no log store.
func TestNewPipelineRecorder_NilWithoutAWorker(t *testing.T) {
	if rec := newPipelineRecorder(nil, LogBodyFull); rec != nil {
		t.Errorf("recorder = %v, want nil when there is nowhere to write", rec)
	}
}
