package types

const SearchServiceKind = "SearchService"

type SearchServiceDefinition struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
	Kind       string            `yaml:"kind" json:"kind"`
	Metadata   Metadata          `yaml:"metadata" json:"metadata"`
	Spec       SearchServiceSpec `yaml:"spec" json:"spec"`
}

type SearchServiceSpec struct {
	Provider  string           `yaml:"provider" json:"provider"`
	Scenarios []SearchScenario `yaml:"scenarios,omitempty" json:"scenarios,omitempty"`
	Faults    SearchFaults     `yaml:"faults,omitempty" json:"faults,omitempty"`
}

type SearchFaults struct {
	LatencyMs      int                        `yaml:"latency_ms,omitempty" json:"latency_ms,omitempty"`
	StatusCode     int                        `yaml:"status_code,omitempty" json:"status_code,omitempty"`
	MalformedJSON  bool                       `yaml:"malformed_json,omitempty" json:"malformed_json,omitempty"`
	Disconnect     bool                       `yaml:"disconnect,omitempty" json:"disconnect,omitempty"`
	PartialResults *SearchPartialResultsFault `yaml:"partial_results,omitempty" json:"partial_results,omitempty"`
}

type SearchPartialResultsFault struct {
	MaxResults int `yaml:"max_results" json:"max_results"`
}

type SearchScenario struct {
	Name     string         `yaml:"name" json:"name"`
	Match    SearchMatch    `yaml:"match,omitempty" json:"match,omitempty"`
	Response SearchResponse `yaml:"response" json:"response"`
}
type SearchMatch struct {
	QueryContains string `yaml:"query_contains,omitempty" json:"query_contains,omitempty"`
	QueryRegex    string `yaml:"query_regex,omitempty" json:"query_regex,omitempty"`
	Default       bool   `yaml:"default,omitempty" json:"default,omitempty"`
}
type SearchResponse struct {
	Answer  string         `yaml:"answer,omitempty" json:"answer,omitempty"`
	Results []SearchResult `yaml:"results,omitempty" json:"results,omitempty"`
}
type SearchResult struct {
	Title      string  `yaml:"title" json:"title"`
	URL        string  `yaml:"url" json:"url"`
	Content    string  `yaml:"content" json:"content"`
	Score      float64 `yaml:"score" json:"score"`
	RawContent *string `yaml:"raw_content,omitempty" json:"raw_content,omitempty"`
	Favicon    string  `yaml:"favicon,omitempty" json:"favicon,omitempty"`
}
