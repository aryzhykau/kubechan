# KubeChan documentation

Use this index to distinguish current documentation from historical plans.

## Current sources of truth

| Document | Use it for |
|---|---|
| [`../AGENTS.md`](../AGENTS.md) | Agent constraints, change rules, and required verification |
| [`../README.md`](../README.md) | Product capabilities and local quick start |
| [`codebase/ARCHITECTURE.md`](codebase/ARCHITECTURE.md) | Runtime flow, service ownership, and invariants |
| [`contracts/investigation-analysis.md`](contracts/investigation-analysis.md) | Evidence, graph, citation, and assessment contract |
| [`codebase/STRUCTURE.md`](codebase/STRUCTURE.md) | Repository layout and module boundaries |
| [`codebase/TESTING.md`](codebase/TESTING.md) | Test commands and validation expectations |
| [`codebase/INTEGRATIONS.md`](codebase/INTEGRATIONS.md) | Kubernetes, SQLite, LLM provider, and WebSocket integrations |
| [`codebase/CONVENTIONS.md`](codebase/CONVENTIONS.md) | Language and repository conventions |
| [`releases/v1.8.0.md`](releases/v1.8.0.md) | v1.8.0 release notes and upgrade considerations |

## Snapshot agent and improvement ideas

- [Snapshot agent contract](contracts/snapshot-agent.md)
- [Improvement backlog](ideas.md)

## Point-in-time audits

[`codebase/CONCERNS.md`](codebase/CONCERNS.md) is a risk register captured during a codebase audit. Validate every item against the current implementation before acting on it; it is not an executable contract.

## Historical material

Documents in [`plans/`](plans/), [`../full-plan.md`](../full-plan.md), and [`project-description.md`](project-description.md) capture design intent at a point in time. They are useful for background and rationale, but they are not authoritative descriptions of the current implementation. Phase names do not imply that a feature is incomplete or current.

When sources disagree, prefer:

1. Executable contracts and tests.
2. `AGENTS.md` and `docs/contracts/`.
3. Current codebase documentation.
4. README.
5. Historical plans.

## Maintenance rule

A change is incomplete when it changes a public API, CRD, service boundary, evidence schema, graph semantics, local workflow, or required validation without updating the corresponding current document.
