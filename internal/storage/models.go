package storage

// InteractionLog represents a single request/response interaction record.
type InteractionLog struct {
	ID             int64  `json:"id"`
	Timestamp      string `json:"timestamp"`
	TenantID       string `json:"tenant_id,omitempty"`
	AgentName      string `json:"agent_name"`
	SessionID      string `json:"session_id"`
	Protocol       string `json:"protocol"`
	RequestMethod  string `json:"request_method"`
	RequestPath    string `json:"request_path"`
	RequestBody    string `json:"request_body,omitempty"`
	ResponseStatus int    `json:"response_status"`
	ResponseBody   string `json:"response_body,omitempty"`
	LatencyMs      int64  `json:"latency_ms"`
	ToolCallsCount int    `json:"tool_calls_count"`
	Streaming      bool   `json:"streaming"`
	Error          string `json:"error,omitempty"`
	ScenarioName   string `json:"scenario_name,omitempty"`
	// Truncated reports that the request and/or response body exceeded
	// the capture cap and the stored body is clipped, so a consumer
	// knows the persisted body is not the complete payload.
	Truncated bool `json:"truncated,omitempty"`
}

// InteractionFilter specifies query criteria for log retrieval.
type InteractionFilter struct {
	TenantID       string
	FilterTenantID bool
	AgentName      string
	SessionID      string
	Since          string // ISO 8601 timestamp
	Until          string // ISO 8601 timestamp
	Limit          int
	Offset         int
}

// DefaultLimit is the default number of log entries returned.
const DefaultLimit = 50

// MaxLimit is the maximum number of log entries per query.
const MaxLimit = 1000
