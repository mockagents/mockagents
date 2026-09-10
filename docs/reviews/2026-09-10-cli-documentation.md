# CLI, contracts, documentation and agent-readiness review

Scope: tracked source at `6ddb03e54a14484e5929a19673f0cfd8a1975f07`, 2026-09-10. Findings concern current behavior; proposed patches below have not been applied. Pass 1 inspected CLI commands, runner/contract/drift implementations, loaders/validators, scaffold, root documentation, Makefile and configuration-reference test. Pass 2 traced CLI → loader → validator → runner → engine session state, contract extraction → comparison → exit status, and documentation → CI/release configuration.

## Findings

| ID | Severity | Evidence | Result / impact |
|---|---|---|---|
| CL-01 | High; release blocker | `cmd/mockagents/test.go:61-75,85,126,185-190`; `internal/config/loader.go:168-182`; `internal/config/testsuite_validator.go:28,93` | Load errors are printed and discarded; suite and pipeline semantic validators are not called. An empty suite exits 0; a directory with malformed YAML plus one passing suite also exits 0. CI can approve tests it never executed. |
| CL-02 | Medium | `internal/runner/runner.go:130` | Session key is only suite name + case name. Running the same suite twice on one Runner, or distinct files with matching names, shares old turn state. Reproduced first run passing a turn-1 assertion and second run failing it. |
| CL-03 | High; release blocker for advertised contract gate | `internal/contract/contract.go:183-232`; `cmd/mockagents/contract.go:120-125` | Schema comparison only inspects top-level `required` and `properties`. Changing `additionalProperties:true` to `false` returns “No changes detected” and exit 0. Root `type`, `enum`, combinators and other constraints can also escape the gate. |
| CL-04 | Medium | `.gitignore:1-12`; absence of tracked `AGENTS.md`; `ARCHITECTURE.md`; `CONTRIBUTING.md:85-89`; `docs/RELEASING.md:22,172-196,223` | A useful architecture map exists, but `.gitignore` deliberately excludes the portable agent instruction file. Release runbook claims current versions are 0.4.0 while manifests are 0.5.0, says GoReleaser floats to latest while workflow pins v2, and contributor docs claim a macOS CI leg that no longer exists. |
| CL-05 | Low | `cmd/mockagents/config_docs_test.go:38-65` | Env-doc test walks arbitrary ignored directories, failing on local `.pytest_cache` ACL before evaluating source. Reproduced failure is environment-sensitive, not evidence of an application defect or clean-checkout CI failure. |

## CL-01: fail closed before execution

Make parse/config failures exit 2 using existing root error handling. Preserve assertion failures as exit 1. Do not silently skip invalid agents in a test gate. In `runTest`, return an aggregate error for `loadErrs`; validate every loaded agent and pipeline before registry insertion. In `loadSuitesFrom`, return directory load errors instead of discarding them. Before running suites call `config.ValidateTestSuite` on each definition/node and validate target references against the loaded registries. The existing validators are the authoritative rules; do not duplicate them.

```go
// cmd/mockagents/test.go; add errors import.
if len(loadErrs) != 0 {
    return fmt.Errorf("loading agents: %w", errors.Join(loadErrs...))
}
// In the agent loop, replace print-and-continue with return errList.
// Before each pipelineReg.Register:
if errs := config.ValidatePipeline(r.Definition, r.FilePath, r.Node); errs != nil {
    return errs
}
// Before ANY RunSuite call:
for _, suite := range suites {
    if errs := config.ValidateTestSuite(suite.Definition, suite.FilePath, suite.Node); errs != nil {
        return errs
    }
}
// In the directory branch of loadSuitesFrom:
if len(errs) != 0 {
    return nil, fmt.Errorf("loading suites: %w", errors.Join(errs...))
}
```

Add a `len(suite.Spec.Cases)==0` guard inside public `Runner.RunSuite` as defense for callers bypassing CLI validation. Decide explicitly whether assertion-free cases are intended smoke tests; do not change that behavior accidentally. Run validation over all inputs before emitting success for any case. Unknown `--format` should return a config error rather than fall back to text. Where RunSuite itself errors, emit a synthetic failed/error result for JSON/JUnit so reports contain the rejected suite as well as a nonzero process status.

Required subprocess tests: valid+malformed directory → 2; empty cases → 2; duplicate case names → 2; invalid pipeline → 2; invalid unrelated agent → 2; valid failing assertion → 1; all valid passing → 0. Test explicit file, directory, glob and default-discovery entry paths. Document exit semantics in `site/docs/guides/testing-agents.md` and CLI help.

## CL-02: isolate test executions

Introduce an atomic per-Runner run counter and use run ordinal plus case index in the session ID, retaining a single ID across turns of one case. Make the namespace unique across Runner instances sharing an engine too (a constructor nonce or engine-owned execution-ID allocator). This avoids collision even when names repeat. Ensure completed test sessions are cleaned up, including per-node pipeline sessions, or use a per-run state store if the execution interface permits it.

```go
// Proposed API shape; add the allocator to Runner and pass IDs explicitly.
runID := r.executionIDs.Next() // unique across runners sharing an Engine
for i := range suite.Spec.Cases {
    sessionID := fmt.Sprintf("test::%s::%d", runID, i)
    cr := r.runCase(suite, &suite.Spec.Cases[i], sessionID)
    // existing aggregation
}
```

Tests: repeat one suite twice on one Runner; run same-named suites from different paths; concurrent executions; shared engine across Runner instances; pipeline node sessions; preserve multi-turn turn-number progression inside each case. Document execution isolation and that YAML runner drives mock user turns rather than executing an external real agent program.

## CL-03: cover the complete input-schema contract

Keep detailed property/required diagnostics, then compare all remaining root-level schema keywords conservatively. Removing a constraint can later be classified additive with explicit keyword rules; an unknown changed constraint must never mean “no change.” Exclude only explicitly non-validating annotations. Apply equivalent handling recursively if detailed classification replaces the current whole-property comparison.

```go
// internal/contract/contract.go, after existing required/property comparisons.
func rootConstraints(schema map[string]any) map[string]any {
    out := make(map[string]any)
    for k, v := range schema {
        switch k {
        case "required", "properties", "title", "description", "$comment", "examples", "default":
            continue
        default:
            out[k] = v
        }
    }
    return out
}
// inside diffTool:
if !reflect.DeepEqual(rootConstraints(old.Parameters), rootConstraints(new.Parameters)) {
    changes = append(changes, Change{
        Severity: SeverityBreaking,
        Path: "tools." + name + ".parameters",
        Message: "root schema constraints changed; compatibility review required",
    })
}
```

Also validate extracted contract JSON shape before diffing: `{}` must not masquerade as a meaningful contract. Document the conservative supported subset and how unsupported constraints are classified. Add table-driven unit and executable exit-code tests for additionalProperties true→false, type change, enum narrowing, oneOf/anyOf change, patternProperties, min/maxProperties, nested constraints, annotation-only edits, unchanged schemas, and known additive optional properties. Preserve existing deterministic output tests.

## CL-04: publish one small agent entry point and repair runbooks

Remove the `AGENTS.md` ignore rule; add a tracked root file linking `ARCHITECTURE.md`, `CONTRIBUTING.md`, `SECURITY.md`, configuration reference and release checklist. Keep local credentials, private runbooks and tool settings ignored. Avoid copying the entire architecture into multiple agent files.

Suggested root `AGENTS.md` content:

```markdown
# Working in MockAgents
Read ARCHITECTURE.md for import boundaries and request flow.
Security-sensitive changes: tenancy/, server/route_authz.go, recording/,
agent/pipeline filesystem writes, SDK binary downloaders, and release workflows.
Keep tenant identity in request context; never trust client-selected tenant IDs.
Config/type changes require schema + API + SDK + examples review together.
Verify Go: go vet ./...; go test ./... -count=1 -timeout 5m.
Run race tests on Linux with a C toolchain: go test -race ./... -count=1 -timeout 5m.
Run go run ./tools/driftcheck and go run ./tools/liquidcheck.
Build ./cmd/mockagents and validate examples/.
For GUI/SDK changes, run the package's build and tests; GUI also needs Playwright.
Release evidence must be for the exact candidate SHA; see docs/RELEASING.md.
Use two review passes: file-level invariants, then cross-component contracts.
Keep generated outputs and credentials out of patches.
```

Replace hardcoded “current version” claims with a single candidate version variable and manifest check; correct GoReleaser version and Linux-only race-job wording. Verify links and copy-paste commands. Treat account/registry setup checkboxes as prerequisites requiring live verification, not established current failures.

## CL-05: scan controlled source roots

Change env reference test to walk `cmd`, `internal`, `sdk/go`, and `tools` (or derive tracked Go files), not arbitrary workspace directories. Preserve fatal errors for unreadable source within those roots; do not blanket-ignore permission errors. Add a small scan test containing an inaccessible/unrelated cache directory plus a real undocumented environment name in an allowed source root. This is a low-priority developer-experience fix.

## Validation evidence

Fresh binary: `go build -o .gotmp/release-audit.exe ./cmd/mockagents` succeeded. Scratch fixtures under `.gotmp/release-audit` used no pre-existing application databases.

```text
empty TestSuite: 0 passed, 0 failed; exit=0
valid + malformed directory: load error printed; 1 passed; exit=0
contract additionalProperties true -> false: No changes detected.; exit=0
same suite supplied twice: first PASS; second expected first, got later; exit=1
validate examples/: 29 files all valid
go vet ./...: PASS
go run ./tools/driftcheck: PASS (35 schemas)
go run ./tools/liquidcheck: PASS
go test ./... -count=1 -timeout 5m: FAIL, cmd configuration walk ACL
go test ./cmd/mockagents -skip '^TestConfigurationReferenceCoversEveryEnvVar$' -count=1 -timeout 5m: PASS
```

Coverage instrumentation attempt failed (`runtime/coverage: package testmain: cannot find package`); no coverage percentage is claimed. Tests without instrumentation passed for runner and contract in the full run. Race execution, PostgreSQL service integration, container, Helm and clean installed-package smoke tests require the release gate environment; local test success does not certify those surfaces.
