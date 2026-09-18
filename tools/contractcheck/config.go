package main

func configure(b *builder, kind string) {
	rate := schema{"type": "number", "minimum": 0, "maximum": 1}
	fault := "types.A2AFaults"
	if kind == "SearchService" {
		fault = "types.SearchFaults"
	}
	b.set(fault, "rate", schema{"anyOf": []any{rate, schema{"type": "null"}}})
	b.set(fault, "latency_ms", schema{"type": "integer", "minimum": 0, "maximum": 60000})
	b.set(fault, "status_code", schema{"anyOf": []any{schema{"const": 0}, schema{"type": "integer", "minimum": 400, "maximum": 599}}})
	opPattern := "^.+$"
	if kind == "SearchService" {
		opPattern = "^/"
	}
	b.set(fault, "operation_rates", schema{"type": "object", "propertyNames": schema{"pattern": opPattern}, "additionalProperties": rate})
	defaultLimit := schema{"contains": schema{"type": "object", "required": []string{"default"}, "properties": schema{"default": schema{"const": true}}}, "minContains": 0, "maxContains": 1}
	if kind == "A2AServer" {
		b.prop("types.Metadata", "name")["maxLength"] = 63
		b.def("types.A2AServerSpec")["required"] = []string{"card"}
		b.def("types.A2AAgentCard")["required"] = []string{"name"}
		b.prop("types.A2AAgentCard", "name")["minLength"] = 1
		b.def("types.A2ASkill")["required"] = []string{"id"}
		b.prop("types.A2ASkill", "id")["minLength"] = 1
		b.set(fault, "timeout_ms", schema{"type": "integer", "minimum": 0, "maximum": 60000})
		b.set(fault, "truncate_after_bytes", schema{"type": "integer", "minimum": 0, "maximum": 1048576})
		b.set(fault, "fixture_rates", schema{"type": "object", "propertyNames": schema{"pattern": "^.+$"}, "additionalProperties": rate})
		b.set(fault, "sequence_rates", schema{"type": "object", "propertyNames": schema{"pattern": "^[1-9][0-9]*$"}, "additionalProperties": rate})
		b.set("types.A2AMessageResponse", "state", schema{"enum": []string{"", "submitted", "working", "input-required", "completed", "canceled", "failed", "rejected"}})
		b.def("types.A2AMessageResponse")["oneOf"] = []any{
			schema{"required": []string{"match"}, "properties": schema{"match": schema{"type": "string", "minLength": 1}, "default": schema{"const": false}}},
			schema{"required": []string{"default"}, "properties": schema{"default": schema{"const": true}, "match": schema{"const": ""}}},
		}
		for k, v := range defaultLimit {
			b.prop("types.A2AServerSpec", "responses")[k] = v
		}
	} else {
		b.def("types.SearchServiceSpec")["required"] = []string{"provider"}
		b.set("types.SearchServiceSpec", "provider", schema{"type": "string", "pattern": "^(?:[tT][aA][vV][iI][lL][yY]|[cC][oO][hH][eE][rR][eE]-[rR][eE][rR][aA][nN][kK]|[oO][pP][eE][nN][aA][iI]-[mM][oO][dD][eE][rR][aA][tT][iI][oO][nN][sS])$"})
		b.def("types.SearchServiceSpec")["if"] = schema{"properties": schema{"provider": schema{"pattern": "^[tT][aA][vV][iI][lL][yY]$"}}}
		b.def("types.SearchServiceSpec")["else"] = schema{"properties": schema{"scenarios": schema{"maxItems": 0}}}
		b.set("types.SearchPartialResultsFault", "max_results", schema{"type": "integer", "minimum": 0, "maximum": 20})
		b.def("types.SearchScenario")["required"] = []string{"name", "match"}
		b.prop("types.SearchScenario", "name")["pattern"] = "\\S"
		b.def("types.SearchMatch")["oneOf"] = []any{
			schema{"required": []string{"query_contains"}, "properties": schema{"query_contains": schema{"minLength": 1}, "query_regex": schema{"const": ""}, "default": schema{"const": false}}},
			schema{"required": []string{"query_regex"}, "properties": schema{"query_regex": schema{"minLength": 1}, "query_contains": schema{"const": ""}, "default": schema{"const": false}}},
			schema{"required": []string{"default"}, "properties": schema{"default": schema{"const": true}, "query_contains": schema{"const": ""}, "query_regex": schema{"const": ""}}},
		}
		scenarios := b.prop("types.SearchServiceSpec", "scenarios")
		scenarios["contains"] = schema{"required": []string{"match"}, "properties": schema{"match": schema{"required": []string{"default"}, "properties": schema{"default": schema{"const": true}}}}}
		scenarios["minContains"] = 0
		scenarios["maxContains"] = 1
		b.def("types.SearchResult")["required"] = []string{"title", "url"}
		b.prop("types.SearchResult", "title")["pattern"] = "\\S"
		b.prop("types.SearchResult", "url")["pattern"] = "^https?://[^/]+"
		b.set("types.SearchResult", "score", rate)
		b.prop("types.SearchResult", "published_date")["pattern"] = "^$|^[0-9]{4}-[0-9]{2}-[0-9]{2}$"
	}
}
