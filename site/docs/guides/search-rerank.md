# Search, rerank, and moderation mocks

R12 brings deterministic retrieval-adjacent services onto the same MockAgents
server: Tavily-shaped declarative search, Cohere v2 reranking, and the existing
OpenAI moderation profile.

## Tavily search

Declare offline search results beside your agents:

```yaml
apiVersion: mockagents/v1
kind: SearchService
metadata:
  name: web-search
spec:
  provider: tavily
  scenarios:
    - name: mockagents-docs
      match:
        query_contains: mockagents
      response:
        answer: MockAgents provides deterministic AI service mocks.
        results:
          - title: MockAgents docs
            url: https://example.test/mockagents
            content: Offline fixture content
            score: 0.95
    - name: safe-empty
      match:
        default: true
      response:
        results: []
```

`POST /search` selects the first case-insensitive `query_contains` or
`query_regex` match, then the explicit default. With no match it returns a safe
empty result set and never makes an upstream request. `max_results` is bounded
to 1–20.

The optional `spec.faults` block applies deterministic service faults:
`latency_ms` (0–60000), `status_code` (400–599), `malformed_json`, `disconnect`,
and `partial_results.max_results` (0–20). Configure one fault behavior per
fixture when you need an unambiguous client test.

The same document kind configures faults for rerank and moderation. Add one
document per provider (metadata names must be unique):

`cohere-rerank.yaml`:

```yaml
apiVersion: mockagents/v1
kind: SearchService
metadata:
  name: cohere-rerank
spec:
  provider: cohere-rerank
  faults:
    status_code: 429
```

`openai-moderations.yaml`:

```yaml
apiVersion: mockagents/v1
kind: SearchService
metadata:
  name: openai-moderations
spec:
  provider: openai-moderations
  faults:
    partial_results:
      max_results: 1
```

All three services use the same precedence: latency, disconnect, HTTP status,
malformed JSON, then partial-result truncation on an otherwise valid response.
HTTP faults retain the provider envelope: Tavily `detail`, Cohere `message`, and
OpenAI `error`.

## Cohere v2 rerank

`POST /v2/rerank` accepts `model`, `query`, `documents`, and optional `top_n`.
MockAgents scores lexical query-token coverage, sorts by descending relevance,
and preserves the original document index when scores tie. IDs and scores are
stable across runs, no model or network is called, and requests are bounded to
1,000 documents.

```bash
curl http://localhost:8080/v2/rerank \
  -H "Content-Type: application/json" \
  -d '{
    "model":"rerank-v4.0-fast",
    "query":"capital united states",
    "documents":[
      "Carson City is a capital",
      "Washington is the capital of the United States"
    ],
    "top_n":1
  }'
```

## OpenAI moderation

`POST /v1/moderations` remains mounted through the common adapter registry. It
supports string, string-array, and content-part inputs with deterministic scores
for all current moderation categories.
