## What and why

<!-- What does this change, and why is it needed? Link the issue it closes. -->

Closes #

## Type of change

<!-- Conventional Commits prefix used in the title: feat, fix, docs, perf, refactor, test, build, ci, chore. -->

- [ ] Bug fix
- [ ] New feature
- [ ] Behaviour change (describe the migration under "Changed" in `CHANGELOG.md`)
- [ ] Documentation only

## Checklist

- [ ] Tests that fail without this change and pass with it (`go test ./<package>` for what you touched; `make test` for the full Go suite)
- [ ] `make lint` passes and Go files are `gofmt`-clean
- [ ] `make validate` passes (when config, types, schemas or examples changed)
- [ ] `make docs-check` passes (when docs, the OpenAPI spec or routes changed)
- [ ] Schema, Go types, validator, examples and `docs/api-spec.yaml` stay in step (when the agent format or an API changed)
- [ ] `make test-python` / `make test-typescript` / `cd sdk/vitest && npm test` (when an SDK changed)
- [ ] `make gui-verify` (when `gui/` changed)
- [ ] `make helm-lint helm-template` (when `deploy/helm` changed)
- [ ] `make bench-report` diff attached (when the engine hot path changed)
- [ ] An entry under `## [Unreleased]` in `CHANGELOG.md` for anything user-visible
- [ ] Every commit is signed off (`git commit -s`) — see [CONTRIBUTING.md](../CONTRIBUTING.md#developer-certificate-of-origin)

## Security-sensitive?

<!-- Does this touch tenancy, authentication/authorization, redaction, quotas, or SDK binary download? If so, say how you checked it. Report vulnerabilities privately instead: see SECURITY.md. -->
