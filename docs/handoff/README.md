# MockAgents developer handoff

This suite describes the local repository at baseline commit `5d5b5172dfd3c19854bcfb1db7d5215af711165e`, analyzed on 2026-09-18. It is a source-based handoff, not a certification of a deployed service, published package, or provider's current API. [Verification evidence and review scope](../reviews/2026-09-18-review-summary.md) distinguish inspected behavior from checks actually executed.

The subsequent [multi-pass documentation review](../reviews/2026-09-18-documentation-review-summary.md) corrected operational and API claims and reproduced unresolved Responses ownership/retention behavior. Its [claim-to-evidence matrix](../reviews/2026-09-18-documentation-integration-checks.md) and [action register](../reviews/2026-09-18-documentation-action-register.md) are the current verification record. Route/model inventories do not establish complete wire-schema coverage or tenant isolation by themselves.

MockAgents runs configurable test doubles for AI provider and agent-integration protocols. An application sends normal SDK requests to local endpoints; fixtures select responses, tool calls, streams, and faults. Pipelines connect mock agent executions. Recording is a separate mode that can contact an upstream service.

## Reading paths

| Reader | Read in order | Outcome |
| --- | --- | --- |
| Product / engineering lead | [Product](product.md), [limitations and behavior](behavior.md), [trade-offs](design-tradeoffs.md) | Understand scope, user journeys, and what the mock can prove |
| New developer | [Onboarding](onboarding.md), [architecture](architecture.md), [codebase map](codebase-map.md), [testing](testing.md) | Build, trace a request, and make a bounded change |
| SDK / integration developer | [API](api.md), [agents](agents.md), [tools](tools.md), [integrations](integrations.md) | Select the right protocol and test its observable behavior |
| Platform operator | [Deployment](deployment.md), [data models](data-models.md), [API security](api.md#authentication-and-tenant-scope) | Understand persistence, tenancy, restart, and scaling limits |
| Orchestration developer | [Orchestration](orchestration.md), [diagrams](diagrams.md), [model dictionary](reference/model-fields.md) | Predict node ordering, state, and partial results |

## Complete suite

- [Product overview and user journeys](product.md)
- [Codebase, component, dependency, and behavior maps](codebase-map.md)
- [Architecture and package boundaries](architecture.md)
- [Architecture, sequence, data-flow, and decision diagrams](diagrams.md)
- [Agent lifecycle and execution](agents.md)
- [Tool interfaces and extensibility](tools.md)
- [Pipelines, multi-agent coordination, and suite runner](orchestration.md)
- [System behavior and known limitations](behavior.md)
- [API reference](api.md) and [complete built-in HTTP route inventory](reference/routes.md)
- [Data model guide](data-models.md) and [Go field dictionary](reference/model-fields.md)
- [SDK, GUI, and external integration map](integrations.md)
- [Design decisions, alternatives, and trade-offs](design-tradeoffs.md)
- [Local development and handoff checklist](onboarding.md)
- [Testing and verification](testing.md)
- [Deployment, operations, and release](deployment.md)
- [Glossary](glossary.md)
- [Tracked source inventory](reference/source-inventory.md) and [direct Go dependency map](reference/dependencies.md)
- [Wiki export instructions](wiki.md)

## Keeping this suite accurate

Field tags come from Go AST extraction; API patterns come from mounting code. Run `go run ./tools/handoffcatalog` after changes to tracked Go files. Regeneration does not infer validation, auth semantics, or complete provider compatibility: update the relevant narrative and trace the implementation too. New untracked source files must be tracked before catalog regeneration includes them.

Use [configuration reference](../../site/docs/reference/configuration.md) for environment defaults, [schemas](../../schema/) for authoring constraints, and [release runbook](../RELEASING.md) for release policy. Avoid creating competing copies of those contracts. The OpenAPI file is intentionally supplemented here because it does not enumerate the entire provider surface.
