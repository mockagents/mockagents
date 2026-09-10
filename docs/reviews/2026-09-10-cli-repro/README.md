# CLI reproduction inputs

Inputs are inert `.txt` files so normal fixture discovery does not treat them as product examples. Copy into a temporary working directory, removing only the final `.txt` extension. Use a freshly built binary from the audited SHA.

1. Put `examples/minimal-agent.yaml` in `agents/`.
2. Run `mockagents test --agents-dir agents empty.yaml`: observed exit 0 with zero cases (CL-01).
3. Put `good.yaml` and `broken.yaml` in `suites/`; run `mockagents test --agents-dir agents suites/`: observed parse error printed, one case passes, exit 0 (CL-01).
4. Run `mockagents contract diff old.json new.json`: observed no changes, exit 0 despite rejecting additional properties in the new schema (CL-03).
5. Put `counter.yaml` in `agents/`; run `mockagents test --agents-dir agents counter-suite.yaml counter-suite.yaml`: first case passes, repeat fails because its session is already on turn 2 (CL-02).

Expected fixed behavior: config/load failures exit 2; constraint tightening is a breaking change/nonzero; repeated suite runs remain independent. Assertions inside a valid suite still exit 1 when they fail. The malformed file is intentionally invalid and must never be silently excluded from a successful run.
