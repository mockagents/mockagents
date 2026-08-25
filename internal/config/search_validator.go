package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/mockagents/mockagents/internal/types"
	"gopkg.in/yaml.v3"
)

const maxSearchLatencyMs = 60_000

func ValidateSearchService(def *types.SearchServiceDefinition, filePath string, node *yaml.Node) *ValidationErrorList {
	ctx := &validationContext{file: filePath, node: node}
	if def.APIVersion != types.AgentAPIVersion {
		ctx.addError("apiVersion", fmt.Sprintf("unsupported version %q", def.APIVersion), "Use apiVersion: mockagents/v1")
	}
	if def.Kind != types.SearchServiceKind {
		ctx.addError("kind", fmt.Sprintf("unsupported kind %q", def.Kind), "Use kind: SearchService")
	}
	if def.Metadata.Name == "" || !metadataNameRe.MatchString(def.Metadata.Name) {
		ctx.addError("metadata.name", "service name must be lowercase kebab-case", "")
	}
	provider := strings.ToLower(def.Spec.Provider)
	if provider != "tavily" && provider != "cohere-rerank" && provider != "openai-moderations" {
		ctx.addError("spec.provider", fmt.Sprintf("unsupported provider %q", def.Spec.Provider), "Use tavily, cohere-rerank, or openai-moderations.")
	}
	if def.Spec.Faults.LatencyMs < 0 || def.Spec.Faults.LatencyMs > maxSearchLatencyMs {
		ctx.addError("spec.faults.latency_ms", fmt.Sprintf("latency_ms must be between 0 and %d", maxSearchLatencyMs), "")
	}
	if rate := def.Spec.Faults.Rate; rate != nil && (*rate < 0 || *rate > 1) {
		ctx.addError("spec.faults.rate", "rate must be between 0 and 1", "")
	}
	for operation, rate := range def.Spec.Faults.OperationRates {
		if !strings.HasPrefix(operation, "/") || rate < 0 || rate > 1 {
			ctx.addError("spec.faults.operation_rates", "operation paths must start with / and rates be between 0 and 1", "")
		}
	}
	if code := def.Spec.Faults.StatusCode; code != 0 && (code < 400 || code > 599) {
		ctx.addError("spec.faults.status_code", "status_code must be 400 through 599", "")
	}
	if p := def.Spec.Faults.PartialResults; p != nil && (p.MaxResults < 0 || p.MaxResults > 20) {
		ctx.addError("spec.faults.partial_results.max_results", "max_results must be between 0 and 20", "")
	}
	defaults := 0
	if provider != "tavily" && len(def.Spec.Scenarios) > 0 {
		ctx.addError("spec.scenarios", "scenarios are only supported by the tavily provider", "Remove scenarios and use the deterministic provider response.")
	}
	for i, scenario := range def.Spec.Scenarios {
		field := fmt.Sprintf("spec.scenarios.%d", i)
		if strings.TrimSpace(scenario.Name) == "" {
			ctx.addError(field+".name", "scenario name is required", "")
		}
		matches := 0
		if scenario.Match.QueryContains != "" {
			matches++
		}
		if scenario.Match.QueryRegex != "" {
			matches++
			if _, err := regexp.Compile(scenario.Match.QueryRegex); err != nil {
				ctx.addError(field+".match.query_regex", "invalid regular expression: "+err.Error(), "")
			}
		}
		if scenario.Match.Default {
			matches++
			defaults++
		}
		if matches != 1 {
			ctx.addError(field+".match", "exactly one of query_contains, query_regex, or default is required", "")
		}
		for j, result := range scenario.Response.Results {
			if strings.TrimSpace(result.Title) == "" || strings.TrimSpace(result.URL) == "" {
				ctx.addError(fmt.Sprintf("%s.response.results.%d", field, j), "title and url are required", "")
			}
			if parsed, err := url.ParseRequestURI(result.URL); err != nil || parsed.Host == "" {
				ctx.addError(fmt.Sprintf("%s.response.results.%d.url", field, j), "url must be an absolute HTTP(S) URL", "")
			} else if parsed.Scheme != "http" && parsed.Scheme != "https" {
				ctx.addError(fmt.Sprintf("%s.response.results.%d.url", field, j), "url must use http or https", "")
			}
			if result.PublishedDate != "" {
				if _, err := time.Parse("2006-01-02", result.PublishedDate); err != nil {
					ctx.addError(fmt.Sprintf("%s.response.results.%d.published_date", field, j), "published_date must use YYYY-MM-DD", "")
				}
			}
			if result.Score < 0 || result.Score > 1 {
				ctx.addError(fmt.Sprintf("%s.response.results.%d.score", field, j), "score must be between 0 and 1", "")
			}
		}
	}
	if defaults > 1 {
		ctx.addError("spec.scenarios", "at most one default scenario is allowed", "")
	}
	if len(ctx.errors) == 0 {
		return nil
	}
	return &ValidationErrorList{Errors: ctx.errors}
}
