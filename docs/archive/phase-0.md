> Historical document. Current behavior is described in [the architecture guide](../architecture.md).

# Phase 0: contracts and evaluation baseline

Phase 0 establishes the behavior that implementation must satisfy. It does not claim that the API, indexer, or UI already runs. The implementation sequence remains in [the project plan](implementation-plan.md).

## Product contract

Given a public GitHub repository URL, OnboardMePlease produces a technical overview before accepting follow-up questions. The overview identifies entry points, runtime units, logical components, data stores, startup and deployment profiles, cross-component flows, unresolved connections, and analysis coverage. It adapts to a CLI, library, service, web app, or a mixed repo without assuming a UI or deployment exists.

Answers focus on the implementation. A behavior trace names the entry point, relevant validation and branches, calls, persistence, events, workers, errors, and response behavior when evidence supports them. Every repository-specific claim points to evidence in one immutable snapshot. An unproved relationship is reported as unresolved.

The inventory accounts for every candidate artifact before overview synthesis. Each artifact has one status: analyzed, excluded, unsupported, failed, or pending. A finished job may still have unsupported or unresolved portions. Directory layout is only a weak hint for component grouping.

## Contract files

| File | Purpose |
|---|---|
| [repository schema](../../contracts/repository.schema.json) | Input kind, immutable snapshot identity, inventory and coverage |
| [evidence schema](../../contracts/evidence.schema.json) | Source ranges, symbols, relationships and provenance |
| [analysis schema](../../contracts/analysis.schema.json) | Claims, technical flows, overview sections and limitations |
| [prompt registry schema](../../contracts/prompt-registry.schema.json) | Versioned prompts, inputs, outputs and budgets |
| [OpenAPI contract](../../contracts/openapi.yaml) | Planned REST endpoints and server-sent progress |
| [prompt baseline](../../prompts/README.md) | Prompt IDs, expected source, and implementation rules |

JSON Schema uses draft 2020-12. API request and response schemas reference these files as design contracts. Generated OpenAPI and Go/TypeScript bindings must be validated together during implementation. This phase favors explicit fields over an attempt to fix every future internal detail.

## Invariants

1. Repository and snapshot IDs scope inventory, retrieval, graph, source, chat, OKF, cache, and export operations. A follow-up cannot silently switch snapshots.
2. Evidence IDs resolve to immutable source ranges or verified process observations. Model-written prose is derived material and never sole support for itself.
3. A `fact` needs source evidence. An `inference` needs source evidence plus a stated assumption. An `unresolved` item identifies the missing connection. Model output cannot change a heuristic edge to a resolved edge.
4. A path is not proof of runtime execution. Import/reference, call, registration, route, publish, and subscribe are separate edge types.
5. The total query budget applies across decomposition and tool calls. Traversal maintains visited state, detects cycles, and reports truncation.
6. The strict local mode has no external model fallback. Embedding/generation is unavailable until a compatible local provider is configured. Cloud mode requires explicit repo-scoped opt-in and filtered payloads.
7. Credentials and secret-bearing source material do not enter model prompts, embeddings, chat persistence, logs, browser responses, OKF, or exports. Detection is fallible; enforced no-egress is the strict local guarantee.
8. Analysis failures are visible by artifact and do not erase findings from other files. The last ready snapshot remains available if a new one fails.
9. No baseline indexing stage executes project build scripts, Git hooks, filters, package installs, or checked-in commands.

## Fixed query limits for the first prototype

| Limit | Initial value | Required behavior |
|---|---:|---|
| Graph depth | 6 edges | Mark frontier at depth limit |
| Expanded nodes | 200 | Stop and report truncated exploration |
| Outgoing candidates per expansion | 20 | Rank by edge relevance; report pruning |
| Follow-up retrieval rounds | 3 | Shared across all subquestions |
| Wall-clock time and model tokens | Configured per deployment | Hard-enforced by Go, not only by prompt |

These are initial resource budgets, not a promise that an entire flow fits within them. The implementation may tune them after evaluating the fixture corpus.

## Fixtures and evaluation

[The mixed fixture](../../testdata/mixed-monolith/README.md) deliberately spreads one checkout flow across TypeScript, Go, Python, SQL, and deployment configuration. It includes a cross-language event connection, a cycle, an unused similarly named function, and an intentionally absent consumer. [The sparse fixture](../../testdata/sparse-repo/README.md) contains no manifest, route, or deployment evidence. [The materializer](../../scripts/materialize_fixtures.py) creates two actual local Git repositories: one committed and one with an unborn HEAD. Expected answers and prohibited claims are in [the evaluation cases](../../testdata/evals/cases.json).

These small fixtures establish behavior and security invariants; they are not performance benchmarks or representative monoliths. The cases are draft until a maintainer reviews the expected findings. Freeze reviewed cases before tuning retrieval. Add larger public or synthetic repositories later, with their revision and expected evidence frozen before tuning.

## Phase 0 exit gate

- Schemas, API contract, prompt registry, fixture code, evaluation cases, architecture decision, capability matrix, and threat model are checked in.
- Schema and reference validation passes offline.
- Every evaluated repository fact refers to an actual fixture source path and range.
- Cases cover scattered structure, a missing connection, a cycle, misleading names, a sparse repo, a prompt injection comment, and a synthetic secret marker.
- README retains the author’s existing notes in place and appends a link to this phase status.

Maintainer review of the draft evaluation answers is an explicit handoff before retrieval tuning. It is not recorded as completed by the offline checker.

Phase 1 begins with a portable service skeleton, database, immutable snapshots, local privacy enforcement, mock providers, and a boot smoke test. No external model credential is needed for Phase 1 tests.
