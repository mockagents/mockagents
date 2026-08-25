# Provider drift detection

R14 starts with an offline, provider-neutral three-way comparison:

```bash
mockagents drift \
  --operation cohere.rerank \
  --adapter internal/adapter/cohere_rerank.go \
  --sdk sdk-shape.json \
  --provider scrubbed-provider-response.json \
  --mock mockagents-response.json \
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

Use `--format json` for automation, `--format sarif` for code scanning, or
`--format junit` for CI test reporting. Use `--output` to write an artifact. The
schedule-only `provider-drift.yml` workflow currently exercises a scrubbed
offline Cohere baseline. Credentialed collectors, headers/enums/error/event
comparisons, versioned baselines, and expiring exceptions remain later R14
slices.
