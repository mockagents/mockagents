package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mockagents/mockagents/internal/engine"
	"github.com/mockagents/mockagents/internal/storage"
)

// pipelineRecorder writes one interaction-log row per pipeline node execution.
//
// Until this existed, a pipeline run left no trace at all: InteractionCapture
// is HTTP middleware and the executor calls the engine in process, so a run
// that exercised three agents was invisible to the log while a single curl was
// not. The console's own framing — "runs against active configuration" —
// invites the expectation that a run leaves the same trace as the traffic it
// simulates, and now it does.
//
// The rows are deliberately NOT dressed up as HTTP requests. There was no
// method, no path and no status code, so those fields stay empty and the row
// carries storage.SourcePipeline instead. Inventing a 200 and a plausible path
// would make a pipeline node indistinguishable from provider traffic, which is
// a worse failure than the invisibility it replaces.
type pipelineRecorder struct {
	worker   *LogWorker
	bodyMode LogBodyMode
}

// newPipelineRecorder returns nil when there is nowhere to write, so the
// executor's nil check does the right thing without a null-object dance.
func newPipelineRecorder(worker *LogWorker, mode LogBodyMode) engine.NodeRecorder {
	if worker == nil {
		return nil
	}
	return &pipelineRecorder{worker: worker, bodyMode: mode}
}

// pipelineNodeInput is the recorded request body for a node.
//
// A node is handed a plain string, not a provider payload, so there is no wire
// body to store. This envelope is that string in the JSON shape the column
// holds everywhere else — readable in the console, parseable by anything that
// already reads these bodies, and honest about being the node's input rather
// than a request someone sent.
type pipelineNodeInput struct {
	Pipeline string `json:"pipeline"`
	Node     string `json:"node"`
	Input    string `json:"input"`
}

func (r *pipelineRecorder) RecordNode(_ context.Context, n engine.NodeInteraction) {
	entry := &storage.InteractionLog{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		TenantID:  n.TenantID,
		AgentName: n.AgentName,
		SessionID: n.SessionID,
		Protocol:  n.Protocol,
		Source:    storage.SourcePipeline,
		LatencyMs: n.Latency.Milliseconds(),
		// No HTTP request happened. Leaving method, path and status at their
		// zero values is the point: a reader who assumes an HTTP row would draw
		// the wrong conclusions from a fabricated 200.
		ResponseStatus: 0,
	}
	if n.Err != nil {
		entry.Error = n.Err.Error()
	}
	if n.Response != nil {
		entry.ScenarioName = n.Response.ScenarioName
		entry.ToolCallsCount = len(n.Response.ToolCalls)
	}

	// Bodies follow the same policy as the HTTP path (SEC-05). A run is not a
	// loophole around a deployment's capture settings.
	if r.bodyMode != LogBodyNone {
		if raw, err := json.Marshal(pipelineNodeInput{
			Pipeline: n.PipelineName,
			Node:     n.NodeID,
			Input:    n.Input,
		}); err == nil {
			entry.RequestBody = r.applyBodyMode(string(raw))
		}
		if n.Response != nil {
			if raw, err := json.Marshal(n.Response); err == nil {
				entry.ResponseBody = r.applyBodyMode(string(raw))
			}
		}
	}

	// Submit is non-blocking and drops rather than stalls when the queue is
	// full, which matters more here than on the HTTP path: this runs on the
	// pipeline's own goroutine, so a blocking write would surface as latency
	// the engine never actually spent.
	r.worker.Submit(entry)
}

func (r *pipelineRecorder) applyBodyMode(body string) string {
	if r.bodyMode == LogBodySanitized {
		return storage.SanitizeBody(body)
	}
	return body
}
