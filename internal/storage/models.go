package storage

// InteractionLog represents a single request/response interaction record.
type InteractionLog struct {
	ID             int64    `json:"id"`
	Timestamp      string   `json:"timestamp"`
	TenantID       string   `json:"tenant_id,omitempty"`
	AgentName      string   `json:"agent_name"`
	SessionID      string   `json:"session_id"`
	Protocol       string   `json:"protocol"`
	RequestMethod  string   `json:"request_method"`
	RequestPath    string   `json:"request_path"`
	RequestBody    string   `json:"request_body,omitempty"`
	ResponseStatus int      `json:"response_status"`
	ResponseBody   string   `json:"response_body,omitempty"`
	LatencyMs      int64    `json:"latency_ms"`
	ToolCallsCount int      `json:"tool_calls_count"`
	Streaming      bool     `json:"streaming"`
	Error          string   `json:"error,omitempty"`
	ScenarioName   string   `json:"scenario_name,omitempty"`
	ChaosAction    string   `json:"chaos_action,omitempty"`
	ChaosSource    string   `json:"chaos_source,omitempty"`
	ChaosSeed      *int64   `json:"chaos_seed,omitempty"`
	ChaosRate      *float64 `json:"chaos_rate,omitempty"`
	// Truncated reports that the request and/or response body exceeded
	// the capture cap and the stored body is clipped, so a consumer
	// knows the persisted body is not the complete payload.
	Truncated bool `json:"truncated,omitempty"`
	// Source says what produced this interaction. Not every row is an HTTP
	// request any more: a pipeline run drives the engine in process, and the
	// results are real interactions that ought to be visible — but a reader
	// who assumes an HTTP row would draw the wrong conclusions from an empty
	// method, path and status.
	//
	// One of SourceHTTP or SourcePipeline. Rows written before this column
	// existed read back as SourceHTTP, which is what they were.
	Source string `json:"source,omitempty"`
}

// Interaction sources. Stable wire values.
const (
	// SourceHTTP: a request served through a provider endpoint. Carries a
	// method, path and response status.
	SourceHTTP = "http"
	// SourcePipeline: one node of a pipeline run. The engine was called in
	// process, so there is no HTTP method, path or status to record, and those
	// fields are left empty rather than filled with plausible values.
	SourcePipeline = "pipeline"
)

// InteractionFilter specifies query criteria for log retrieval.
type InteractionFilter struct {
	TenantID       string
	FilterTenantID bool
	AgentName      string
	SessionID      string
	// SessionPrefix matches every session id STARTING WITH this value.
	//
	// A pipeline run scopes each node's session as
	// "<session>::<pipeline>::<node>", so the id an operator submitted never
	// equals any id in the log. Exact equality therefore cannot answer "show me
	// what this run did", which is the one question a run's evidence leads to.
	// Ignored when SessionID is set: an exact id is the more specific request.
	SessionPrefix string
	Since         string // ISO 8601 timestamp
	Until         string // ISO 8601 timestamp
	Limit         int
	Offset        int
}

// DefaultLimit is the default number of log entries returned.
const DefaultLimit = 50

// MaxLimit is the maximum number of log entries per query.
const MaxLimit = 1000
