# Provider drift detection

R14 starts with an offline, provider-neutral three-way comparison:

```bash
mockagents drift \
  --operation cohere.rerank \
  --adapter internal/adapter/cohere_rerank.go \
  --sdk sdk-shape.json \
  --provider scrubbed-provider-response.json \
  --mock mockagents-response.json \
  --sdk-headers sdk-headers.json \
  --provider-headers provider-headers.json \
  --mock-headers mock-headers.json \
  --sdk-enums sdk-enums.json \
  --provider-enums provider-enums.json \
  --mock-enums mock-enums.json \
  --sdk-events sdk-events.json \
  --provider-events provider-events.json \
  --mock-events mock-events.json \
  --sdk-errors sdk-errors.json \
  --provider-errors provider-errors.json \
  --mock-errors mock-errors.json \
  --ignore-path '$.created' \
  --format markdown
```

The command never reads provider credentials or makes network calls. Collection
is a separate, explicit step so raw prompts and secrets cannot accidentally
enter pull-request jobs or reports.

The canonicalizer recursively compares JSON field presence, types, array item
shapes, and nullability. Findings are deterministic and ordered by JSON path:

- `critical`: an SDK-required field is missing, or SDK/provider/mock types or
  nullability disagree. The command exits nonzero.
- `warning`: a field exists only in the provider response. This is treated as
  an additive provider change.
- `info`: a field exists only in MockAgents.

Repeat `--ignore-path` to exclude volatile paths from all three inputs. Each
value uses the JSON-path notation shown in reports and excludes the named path
plus descendants; for example, `$.created` and `$.data[].request_id`.

To compare response headers, provide all three optional header artifacts. Each
must be a JSON object. Header names are normalized case-insensitively and appear
under `$headers`, so `--ignore-path '$headers.date'` can suppress a volatile
header without hiding body drift.

Enum inventories are also an optional all-or-none trio. Each artifact maps a
report path to its complete supported string values, for example
`{"$.status":["queued","running","done"]}`. Missing SDK-required values are
critical, provider-only values warn, and mock-only values are informational.

Stream event artifacts are an optional all-or-none trio of JSON string arrays.
Their order and duplicates are significant: any SDK/provider/mock sequence
mismatch is critical and appears at `$events`. Use `--ignore-path '$events'`
when an operation intentionally has no stable stream-order contract.

Error contracts are an optional all-or-none trio keyed by stable case name.
Each case declares `status`, `code`, and `body`; for example,
`{"rate_limit":{"status":429,"code":"rate_limit","body":{"error":{"message":"wait"}}}}`.
The comparison catches missing cases, exact status/code mismatches, and nested
body-shape drift under `$errors.<case>.body`.

Use `--format json` for automation, `--format sarif` for code scanning, or
`--format junit` for CI test reporting. Use `--output` to write an artifact. The
schedule-only `provider-drift.yml` workflow currently exercises a scrubbed
offline Cohere baseline. Credentialed collectors, versioned baselines, and
expiring exceptions remain later R14
slices.
