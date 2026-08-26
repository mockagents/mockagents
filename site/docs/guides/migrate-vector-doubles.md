# Migrating from separate vector-service doubles

RAG tests often replace the vector layer with an in-process fake or start a
second mock service beside the LLM mock. MockAgents can serve deterministic
Qdrant, Pinecone, and Chroma v2 profiles from the same process as the model
API. Your retrieval adapter keeps speaking its provider protocol, while the
test data moves into a validated `VectorCollection` fixture.

This guide migrates the test boundary, not the production database. Production
continues to use its real vector service; tests point the same adapter or client
at MockAgents.

## Before: an application-owned retrieval fake

```python
class FakeVectorStore:
    def search(self, vector, limit, team):
        if team == "support":
            return [{
                "id": "refund-policy",
                "score": 0.99,
                "metadata": {"team": "support"},
            }]
        return []


def test_refund_context():
    context = build_context(
        store=FakeVectorStore(),
        query_vector=[1, 0, 0],
        team="support",
    )
    assert "refund-policy" in context
```

This checks application logic against the fake's private return type. It does
not exercise provider request fields, filter encoding, response parsing,
dimension errors, result ordering, or the network path used in production.

## After: a declarative collection

Create `agents/support-docs.yaml`:

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
        title: Refund policy
    - id: billing-policy
      vector: [0, 1, 0]
      metadata:
        team: finance
        title: Billing policy
```

Validate and start the same directory that contains your LLM Agent fixtures:

```bash
mockagents validate agents
mockagents start --agents-dir agents --port 8080
```

Point the production retrieval adapter at the matching provider profile. For a
Qdrant-shaped adapter, the query remains an ordinary provider request:

```python
import httpx

def search_support(vector):
    response = httpx.post(
        "http://127.0.0.1:8080/collections/support-docs/points/search",
        json={
            "vector": vector,
            "limit": 5,
            "filter": {
                "must": [{"key": "team", "match": {"value": "support"}}]
            },
        },
    )
    response.raise_for_status()
    return response.json()["result"]
```

Use dependency injection or a test-only base URL to redirect the existing
adapter. Do not add a second retrieval implementation just for MockAgents.

## Choose the profile that matches production

| Production dependency | MockAgents test base | Fixture mapping |
|---|---|---|
| Qdrant | `http://127.0.0.1:8080` | Collection name maps to `/collections/{name}` |
| Pinecone | `http://127.0.0.1:8080/pinecone/{index}` | Index name maps to the collection; the empty namespace shares fixture points |
| Chroma v2 | `http://127.0.0.1:8080/api/v2/tenants/default_tenant/databases/default_database` | Collection name maps to the default tenant/database scope |

Keep the application's provider selection unchanged. Switching from a
Qdrant-shaped production adapter to a Pinecone-shaped test adapter would hide
the contract you intend to test.

## Translate fake behavior into fixtures and faults

| Separate-double behavior | MockAgents representation |
|---|---|
| Hard-coded search results | Points with deterministic vectors and metadata |
| Branch by namespace | Pinecone namespace or tenant-scoped collection data |
| Branch by metadata | Provider equality filters over point metadata |
| Preserve a particular order | Scores first, then stable point ID for ties |
| Return no matches | Orthogonal vectors, contradictory filters, or a score threshold |
| Return fewer results than requested | `faults.partial_results.max_results` |
| Throw timeout/rate-limit/server errors | Deterministic vector chaos policy |
| Reject malformed vectors | Declared dimensions and provider-shaped validation errors |
| Reset mutable fake state | Restart the test server or reseed declarative fixtures |

Prefer data that makes the ranking outcome evident. For example, orthogonal
unit vectors make a cosine query's expected winner readable without duplicating
the ranking algorithm in the test.

## Exercise retrieval failures explicitly

An empty page and a partial page are different application conditions. Use an
empty result to test fallback behavior; configure a partial-result fault to
test a valid but truncated provider response:

```yaml
spec:
  dimension: 3
  metric: cosine
  faults:
    seed: 42
    rate: 1
    partial_results:
      max_results: 1
  points:
    - id: first
      vector: [1, 0, 0]
    - id: second
      vector: [0.9, 0.1, 0]
```

Request-level `X-Mockagents-Chaos: off` suppresses configured vector chaos for
one query. `X-Mockagents-Chaos: partial` forces a configured partial-result
fault. The response headers expose the effective action and source so a retry
or fallback test can verify why the result changed.

Also retain tests for missing collections, dimension mismatches, invalid
filters, duplicate/deleted IDs, and excessive top-k values. These should pass
through the application's real error handling rather than being raised by a
fake method.

## Runtime mutation versus declarative state

Prefer `VectorCollection` files for stable test data: they are reviewable,
validated before startup, and repeatable across runs. Use provider upsert and
delete routes only when mutation is part of the behavior under test.

When tests mutate collections, isolate them by server lifecycle, authenticated
tenant, or unique collection/namespace. Do not rely on test order or a shared
process-global cleanup hook.

## Migrate incrementally

1. Inventory the fake's queries, filters, namespaces, mutations, and failures.
2. Select the MockAgents profile matching the production provider.
3. Convert stable records into a validated `VectorCollection`.
4. Redirect the existing adapter's base URL in one test group.
5. Replace fake-specific assertions with application outcomes and interaction
   assertions.
6. Add empty, partial, invalid-dimension, and provider-error cases.
7. Block external vector and model egress in CI, then remove the old service.

Keep a separate double temporarily for an unsupported provider operation. Do
not weaken a test to a different provider profile merely to finish migration;
port supported operations first and document the remaining boundary.

## Completion checklist

1. `mockagents validate agents` accepts every collection and Agent fixture.
2. The test uses the same provider-shaped retrieval adapter as production.
3. LLM and vector fixtures start from one MockAgents process.
4. Ranking, filters, namespaces, and error paths have explicit coverage.
5. Mutable tests are isolated and do not depend on execution order.
6. CI blocks live provider egress and remains deterministic.
