# `mockagents-test` — GitHub Actions composite action

Install the MockAgents CLI, validate your agent definitions, and run a
TestSuite with JUnit XML output in one step. The generated report is
exposed as a step output so any downstream JUnit reporter action can
pick it up.

## Minimal usage

```yaml
jobs:
  agent-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Run MockAgents tests
        id: mockagents
        uses: mockagents/mockagents/deploy/actions/mockagents-test@main
        with:
          agents-dir: ./agents
          suites: ./tests

      - name: Publish JUnit report
        if: always()
        uses: mikepenz/action-junit-report@v5
        with:
          report_paths: ${{ steps.mockagents.outputs.junit-report }}
```

## Inputs

| Name           | Default                | Purpose                                                                 |
| -------------- | ---------------------- | ----------------------------------------------------------------------- |
| `version`      | `latest`               | mockagents CLI version. Pin to a tag (e.g. `v0.1.0`) for reproducibility. |
| `agents-dir`   | `./agents`             | Directory of `kind:Agent` YAML definitions.                             |
| `suites`       | *(same as agents-dir)* | Path to a TestSuite file or a directory of TestSuites.                  |
| `junit-output` | `mockagents-junit.xml` | Where the JUnit XML report is written.                                  |
| `go-version`   | `1.26`                 | Go version used to install the CLI via `go install`.                    |
| `skip-validate`| `false`                | Set to `true` to skip `mockagents validate` before running tests.       |

## Outputs

| Name            | Purpose                                |
| --------------- | -------------------------------------- |
| `junit-report`  | Absolute path to the generated report. |

## Exit codes

- **0** — every TestSuite case passed
- **1** — at least one assertion failed (JUnit XML still written)
- **2** — configuration error (missing binary, validate failure, etc.)

## Why composite and not Docker?

Composite actions run on the runner without pulling a container, so
cache layers are shared with other Go steps and startup is measured in
seconds rather than minutes. The trade-off is that the runner needs a
Go toolchain, which is free to provision via `actions/setup-go@v5` and
already required by most Go-oriented CI pipelines.
