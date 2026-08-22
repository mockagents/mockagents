# Search, rerank, and moderation mocks

R12 brings deterministic retrieval-adjacent services onto the same MockAgents
server. The first slice includes Cohere v2 reranking and the existing OpenAI
moderation profile; Tavily-shaped declarative search fixtures are in progress.

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
