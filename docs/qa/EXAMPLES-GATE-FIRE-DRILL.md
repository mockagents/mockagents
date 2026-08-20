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
`frameworks/agents/support-agent.yaml`. That document had been checked by
**nothing**: `mockagents validate examples/` does not recurse
(`listDocumentPaths` skips directories), and the old `os.listdir` walk did not
either. The framework recipe jobs execute it, but executing an agent does not
assert that it has a catch-all scenario.

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

## Cleanup

Mutations were applied and reverted from byte-exact backups in the scratch
directory; `git status` confirms only the intended files changed, and no drill
artifact remains (`drill-unknown-kind.yaml`, `drilldir/`, or the planted
`node_modules/drill-pkg`).
