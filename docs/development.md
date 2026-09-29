# Development and verification scope

Build with `go build ./...`. The runtime requires Git and the compiled binary; SQLite is embedded and does not require CGO. Follow AGENTS.md for graphify navigation and refresh after code changes. Do not index `.onboard` outputs as source.

The migration baseline is `c6697e2`, branch `offline-cli`. Service source, UI, PostgreSQL/River jobs, provider adapters, prompt registry and service-only scripts were retired. Ignored credentials, data, database volumes and developer configuration were preserved. Historical design documents remain explicitly archived.

The executable contract is the Go result decoder and validation in `internal/engine/knowledge.go`; its agent-facing description lives alongside the canonical skill. Do not add a second provider abstraction or a general agent orchestration framework.

The existing mixed-monolith fixture is the reviewed correctness reference for Go static links, cross-language gaps, ambiguous legacy code and prompt injection in comments. It is not a large-enterprise benchmark. A local Kafka checkout is available as a future text-scale reference (7,564 tracked files observed before migration evaluation), but Java/Scala semantic support is outside this implementation's declared matrix. No repository builds or dependency installations are run by the indexer.

Manual acceptance questions (use the same investigation budget for source-only and CLI-assisted investigation):

1. Which route actually registers paid-order cancellation? Anchor: `server/main.go`; do not select the unused legacy helper.
2. Which preconditions reject cancellation? Anchor: `server/cancel.go`; inspect both paid-state and eligibility checks.
3. Who calls `processCancellation`? Include the handler and audit cycle; terminate traversal cycles.
4. What does `saveOrderState` persist? Anchor: `server/store.go`; it is a stub, not evidence of a database write.
5. What happens if publication fails? Anchor: the return in `processCancellation`; do not assert rollback.
6. Is broker delivery to the Python worker established? Inspect worker and config; topic agreement is not runtime proof.
7. Who consumes `refund.completed`? Record the actual searched scope; absence of a match is not global absence.
8. Is a refund complete before the HTTP 202? Do not conflate the handler and worker timelines.
9. What is the worker's idempotency input? Anchor: `worker/refund_worker.py`; retain the external gateway limitation.
10. Does the browser consume a refund event? Inspect `web/cancel.ts`; do not invent a subscription.
11. Which claims become stale after changing the handler or adding a consumer? Positive citations and negative scopes have different dependencies.
12. Can a same-name method in another package become a false call target? Compiler object identity must keep definitions separate.
13. What happens after a branch switch or delete? Old evidence must not be served as current.
14. What happens when a deep query has no matching knowledge? Preserve useful seeds, inspect source directly and save only a bounded supported finding.

Source-only review already established the fixture's guard, stub, recursion and missing delivery proof. End-to-end accuracy improvements over an agent baseline, a large-repository memory target and two independent agents' reasoning quality require separate measurements; build success does not establish them. Phase 7 embeddings and phase 8 scale hardening remain outside this migration.

Migration checks completed locally: Windows build and Go vet; skill validation; isolated unborn-repository update and no-op reuse; incoming call cycles; explicit no-match; bounded cursor pagination; dry-run and idempotent apply; stale source/result/cursor rejection; claim-level invalidation; negative-scope additions; and portable cache reconstruction. A separate Go fixture distinguished same-name declarations across packages, concrete method dispatch and interface dispatch, and preserved all 601 incoming calls through pagination. Cache reconstruction after adding a negative-scope match retained the old claim as stale.

Codex CLI 0.155.0 and Claude Code 2.1.220 were detected locally. No provider-backed agent sessions were launched, so two-agent behavioral compatibility and disconnected local-model reasoning remain unverified. No local inference runtime was found on PATH.
