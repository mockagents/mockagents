# Examples-gate fire drill (2026-08-20)

**Result: the gate fires — exit 1 on all four mutations, in both partitions —
and the CI step runs the suite for real. Part 2 extends discovery into
subdirectories and shows it collects the one document nothing was checking,
while still ignoring dependency trees. No residual gap.**

## Why this drill exists

`examples/tests/` ran nowhere. The `test-python` job only ran `sdk/python/tests`,
so nothing executed these assertions, and they had rotted to **16 failures out
of 88** without anyone noticing.

The rot was not a broken example. It was that the test parametrized over *every*
`*.yaml` in `examples/` and asserted agent-shaped invariants on all of them,
while `examples/` also holds an `A2AServer`, an `MCPServer`, a `Pipeline` and two
`TestSuite` documents. Fifteen of the sixteen failures were the test being wrong
about its own corpus.

Fixing that meant **narrowing** what the agent assertions run against — exactly
the kind of change that can quietly turn a suite into a no-op. A gate nobody has
watched bite is a gate nobody should trust, so it was made to bite before it was
wired up.

## What was done

Each mutation was applied to a clean tree, run through the **exact command the
new CI step runs**, then reverted from a byte-exact backup:

```bash
cd examples && PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 python -m pytest tests/ -v
```

Baseline before and after every case: **131 passed, 7 skipped, exit 0.** (The
7 skips are the per-kind tests declining files of another kind.)

## Result

| # | Mutation | Exit | Test that caught it |
|---|---|---|---|
| A | `weather-agent.yaml` default scenario given a `match` block | **1** | `test_example_has_default_scenario` |
| B | `realtime-voice-agent.yaml` regains `match: {default: true}` — *the original bug* | **1** | `test_example_has_default_scenario` **+** `test_example_scenario_match_keys_are_declared_in_the_schema` |
| C | `research-suite.yaml` loses `spec.cases` | **1** | `test_non_agent_example_has_required_spec_fields` **+** `test_test_suite_cases_have_names` |
| D | a new `kind: Widget` document appears | **1** | `test_example_is_a_recognized_document` **+** `test_non_agent_example_has_required_spec_fields` |

Four things worth noting:

1. **Both partitions bite.** A and B are agent-only assertions; C and D are
   non-agent ones. The `kind` filter narrowed the agent tests without turning
   the other documents into unchecked files — which was the whole risk.
2. **Case D is the anti-rot case.** A document of a kind nobody taught the test
   about fails loudly instead of falling out of both partitions. It mirrors the
   Go loader, which rejects an unrecognized kind outright.
3. **Case B fails twice, on purpose.** `match: {default: true}` was never a real
   catch-all: `MatchRule` has no `default` field, the Go YAML decoder drops
   unknown keys, and the resulting *empty* rule matches every request — so it
   behaved like a default by accident while the published schema
   (`additionalProperties: false`) rejected it and the match-rate metric never
   counted it as the default. The schema-key assertion names that directly.
4. **`test_partitions_cover_every_example` guards the parametrization itself**,
   so a future bug that emptied a partition is a failure rather than a suite
   that silently tests nothing.

## Verified

- ✅ Every mutation exits non-zero under the exact CI command.
- ✅ Failures are scoped to the mutated file and name it in the message.
- ✅ The tree returns to 131 passed / exit 0 after each revert.
- ✅ **The CI step executes the suite.** First run on `main` (commit `f0e8cb1`,
  [run 32421192487](https://github.com/mockagents/mockagents/actions/runs/32421192487))
  went green on all 12 jobs, and the step's log shows `collected 138 items` →
  `131 passed, 7 skipped` — identical to local. The count is the point: it
  proves the step ran the suite rather than collecting nothing, which is the
  failure mode a `kind` filter could plausibly have introduced.

Still inferred, not observed: that a red *step* fails the Actions *job*. That
inference is safe here — a non-zero pytest exit failing a `run:` step is
GitHub's default, and the sibling `Run tests` step in the same job has been
relied on for exactly that. The drill above supplies the other half: bad input
does produce the non-zero exit.

Re-run this drill whenever the partitioning logic or the set of known kinds
changes.

## Part 2 — recursive discovery (2026-08-20)

Discovery was then made recursive, to cover
`frameworks/agents/support-agent.yaml`. That document was at the time checked by
**nothing**: `mockagents validate examples/` did not recurse
(`listDocumentPaths` skipped directories), and the old `os.listdir` walk did not
either. The framework recipe jobs execute it, but executing an agent does not
assert that it has a catch-all scenario. (Part 3 closes the Go half.)

Recursion brings its own failure mode, and it is the opposite of the last one:
not collecting too little, but collecting **junk**. `examples/frameworks/
typescript/node_modules` ships `.travis.yml` and `FUNDING.yml`, and it is
untracked — so a naive walk collects a corpus that depends on whether anyone
has run `npm install`, i.e. green in a clean CI job and red on a developer's
box. Hence `SKIP_DIRS`, and hence a drill case that plants junk rather than
breaking something:

| # | Mutation | Expected | Exit | Caught by |
|---|---|---|---|---|
| A | the **subdirectory** agent loses its default scenario | red | **1** | `test_example_has_default_scenario[frameworks/agents/support-agent.yaml]` |
| B | a bogus YAML is planted **inside `node_modules`** | **green** | **0** | *nothing — correctly ignored* |
| C | a broken document appears in a **brand-new** subdirectory | red | **1** | `test_example_yaml_is_valid[drilldir/bad.yaml]` **+** `test_example_has_default_scenario` |

Baseline before and after: **138 passed, 7 skipped, exit 0** (up from 131 — the
subdirectory agent adds 7 parametrized cases).

1. **B is the case that matters**, and it is the one that passes. A drill where
   every case goes red would not have tested the exclusion at all; the only way
   to show `SKIP_DIRS` works is to plant something that *must not* be collected
   and confirm the suite stays green.
2. **C proves recursion is general, not a hardcoded path.** A new subdirectory
   nobody anticipated is covered automatically — so this gate does not need
   updating each time examples grow a folder.
3. **Test ids carry the relative path** (`frameworks/agents/support-agent.yaml`),
   normalized to forward slashes so they read the same on Windows and Linux.

## Part 3 — `mockagents validate` recurses (2026-08-20)

Part 2 left the two gates disagreeing: the Python suite saw 28 documents,
`mockagents validate examples/` saw 27. The Go scan
(`internal/config/loader.go listDocumentPaths`) stopped at the top level, so the
frameworks agent got shape checks from Python and no schema or cross-document
checks at all.

Making the scan recursive is not a one-line change, because `listDocumentPaths`
feeds `LoadAllDocuments`, which backs `start`, `mcp`, `a2a` and `test` as well as
`validate` — it decides what the **server serves**, not just what validate reads.
The first attempt proved the point immediately: `.json` is a document extension,
so recursion swallowed `examples/frameworks/typescript/package.json` and
`package-lock.json` and turned a clean tree into **30 files, 10 errors**. Shipped
as-is, `mockagents start ./my-project` would have failed on any Node project.

The rule that resolves it: **the top level of an agents directory is ours by
convention; a subdirectory may belong to a project that merely contains agents.**
So top-level files keep the old contract — any document-extension file there is
meant to be a document, and is reported when it is not — while a nested file is
collected only if it identifies itself, by `apiVersion: mockagents/…` or by a
kind we own. Nested files that are malformed are still collected, so a real
document with a syntax error fails loudly instead of vanishing.

| # | Case | Expected | Exit |
|---|---|---|---|
| A | the subdirectory agent gets an invalid `spec.protocol` | red | **1** |
| B | a broken Agent planted **inside `node_modules`** | **green** | **0** |
| C | restored tree | green | **0** |

Before the change, case A exited **0** — `validate examples/` reported
`Validated 27 file(s)` against 28 tracked documents, which is the direct evidence
it never opened the file. It now reports 28 and names the error with a line
number.

Note what B means for the server, not just for validate: a dependency tree that
happens to contain YAML can no longer stop `mockagents start` from booting.

Unit coverage for the rule lives in `internal/config/loader_recursive_test.go` —
recursion, vendor and dot-directory pruning, nested non-documents ignored,
top-level non-documents still reported, nested malformed documents still
surfaced, and the oversized-file cut-off.

## Cleanup

Mutations were applied and reverted from byte-exact backups in the scratch
directory; `git status` confirms only the intended files changed, and no drill
artifact remains (`drill-unknown-kind.yaml`, `drilldir/`, the planted
`node_modules/drill-pkg`, or `node_modules/evil`).
