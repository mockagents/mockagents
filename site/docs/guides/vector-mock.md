# VectorMock

MockAgents includes in-memory Qdrant- and Pinecone-compatible vector surfaces
on the same server as the LLM APIs. They share one deterministic, bounded,
process-local store and never call a network upstream.

Qdrant, Pinecone, and Chroma v2 profiles are available. Cross-service chaos
remains in progress.

## Load deterministic fixtures at startup

Place a `VectorCollection` document anywhere under `--agents-dir`. Recursive
startup loading validates and seeds it into the same store used by the Qdrant
HTTP routes:

```yaml
apiVersion: mockagents/v1
kind: VectorCollection
metadata:
  name: support-docs
spec:
  dimension: 3
  metric: cosine
  points:
    - id: refund-policy
      vector: [1, 0, 0]
      metadata:
        team: support
```

Run `mockagents validate <directory>` before startup to catch duplicate IDs,
invalid metrics, non-finite values, or vector dimension mismatches. Set
`metadata.tenant_id` to seed the collection only in that tenant's namespace.

To exercise application behavior when a provider returns only part of a valid
ranked page, configure a deterministic collection fault:

```yaml
spec:
  faults:
    partial_results:
      max_results: 1
```

Ranking and filtering happen first, then the response is truncated to
`max_results`. Provider searches set `X-Mockagents-Vector-Partial: true` only
when the fault actually drops matches. `max_results: 0` produces an explicitly
partial empty page.

Add `faults.seed` and `faults.rate` to gate truncation with the same stable
request decision used by search/rerank/moderation. Omitting `rate` keeps the
legacy always-on behavior. `X-Mockagents-Chaos: partial` forces configured
truncation for one query, while `X-Mockagents-Chaos: off` suppresses it. The
request override wins over the seeded rate; ranking and filtering still happen
before truncation. Qdrant, Pinecone, and Chroma responses expose the effective
decision through `X-Mockagents-Chaos-Action` and `X-Mockagents-Chaos-Source`.

## Create and seed a collection

```bash
curl -X PUT http://localhost:8080/collections/docs \
  -H "Content-Type: application/json" \
  -d '{"vectors":{"size":3,"distance":"Cosine"}}'

curl -X PUT 'http://localhost:8080/collections/docs/points?wait=true' \
  -H "Content-Type: application/json" \
  -d '{"points":[
    {"id":"refund","vector":[1,0,0],"payload":{"team":"support"}},
    {"id":"billing","vector":[0,1,0],"payload":{"team":"finance"}}
  ]}'
```

Collections support `Cosine`, `Dot`, and `Euclid`, up to 65,536 dimensions and
100,000 points. An upsert batch is atomic: one invalid dimension rejects the
whole batch. In multi-tenant mode, collection names and mutable point state are
isolated by the authenticated tenant context.

## Query deterministic results

```bash
curl -X POST http://localhost:8080/collections/docs/points/search \
  -H "Content-Type: application/json" \
  -d '{
    "vector":[1,0,0],
    "limit":5,
    "score_threshold":0.5,
    "filter":{"must":[{"key":"team","match":{"value":"support"}}]}
  }'
```

Results sort by descending score, then stable point ID when scores tie. Equality
filters preserve JSON types, contradictory requirements return an empty result,
and `score_threshold` makes low-confidence-only or empty-result tests explicit.

## Supported Qdrant routes

| Route | Purpose |
|---|---|
| `PUT /collections/{collection}` | Create a collection |
| `GET /collections/{collection}` | Inspect dimensions, metric, and point count |
| `DELETE /collections/{collection}` | Delete a collection |
| `PUT /collections/{collection}/points` | Atomic upsert |
| `POST /collections/{collection}/points` | Fetch points by ID |
| `POST /collections/{collection}/points/delete` | Delete points by ID |
| `POST /collections/{collection}/points/search` | Vector search with equality filters |

Malformed JSON, missing collections, dimension mismatches, invalid filters, and
top-k values above 1,000 return Qdrant-shaped error envelopes.

## Pinecone compatibility

Pinecone normally identifies an index through its data-plane host. MockAgents
serves multiple indexes on one port, so use `/pinecone/{index}` as that host's
base path. The remaining routes and request/response shapes follow Pinecone's
data-plane API:

| Route | Purpose |
|---|---|
| `POST /pinecone/{index}/vectors/upsert` | Upsert string-ID vectors into a namespace |
| `POST /pinecone/{index}/query` | Query with topK, `$eq` filters, values, and metadata options |
| `GET /pinecone/{index}/vectors/fetch` | Fetch repeated `ids` query parameters |
| `POST /pinecone/{index}/vectors/delete` | Delete IDs or an entire namespace |
| `POST /pinecone/{index}/describe_index_stats` | Read dimensions, metric, totals, and namespace counts |

The empty Pinecone namespace maps directly to the declarative collection, so
the same fixture is queryable through both provider profiles. Named namespaces
are created on first upsert with the index's declared dimension and metric.

## Chroma v2 compatibility

Chroma uses tenant/database-scoped routes. The default Chroma scope shares
declarative fixtures with Qdrant and Pinecone:

`/api/v2/tenants/default_tenant/databases/default_database/collections/{collection}`

Supported operations include heartbeat, create/get/list/count/delete
collections, and add/upsert/get/query/delete/count records. Runtime collections
learn their dimension from the first valid embedding batch; validation is
atomic, so a bad first batch does not partially initialize the collection.
Chroma query responses use its column-oriented arrays and inherit deterministic
stable-ID ordering and partial-result signaling from the shared vector core.
