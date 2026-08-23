package types

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
