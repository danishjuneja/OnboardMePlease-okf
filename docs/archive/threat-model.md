> Historical document. Current behavior is described in [the architecture guide](../architecture.md).

# Phase 0 threat and data-flow model

## Assets and trust boundaries

Assets: Git credentials, model credentials, database credentials, raw repository snapshots, sanitized searchable evidence, embeddings, chat records, generated OKF, logs, and exports.

Inputs crossing into the backend include GitHub URLs, source code, manifests, comments, user questions, and optional external semantic indexes. Repository content and retrieved context are untrusted data even when a maintainer authored them. The browser is a client; it does not receive service credentials. Git hosts and model providers are distinct external services.

## Data flows

1. Public GitHub clone -> isolated snapshot -> local inventory/parser. Baseline ingestion executes no project code.
2. Local parser -> structured facts and source ranges -> sanitizer -> indexed metadata/graph and optional model context.
3. Sanitized content -> approved local or cloud embedding provider -> vector index. Strict local never selects a cloud endpoint.
4. Authorized query -> scoped retrieval -> sanitizer/egress gate -> selected synthesis provider -> validated answer -> browser.
5. Supported claims -> deterministic OKF renderer -> sanitizer/validator -> export. Raw source remains separate from exported knowledge.

## Threats and required controls

| Threat | Control | Test |
|---|---|---|
| Secret in source, question, or model answer | Local scan, exclusion/redaction, centralized egress and output validation; block uncertain payloads | Synthetic marker never appears in provider capture, vectors input, DB text, logs, UI, OKF or export |
| Secret in release/config | Secret files, protected host storage, ignored paths, no build ARG/ENV, image/content scan | Release artifact and log scan |
| Prompt injection in README/comment | Data/instruction separation, typed tools, server-enforced policy | Fixture comment cannot redirect model or request secrets |
| Cross-repo or cross-snapshot disclosure | Authorization and snapshot filters on every query/API/cache/SSE/export | Isolation tests with identical symbol names |
| Untrusted Git paths or symlinks | Never follow symlinks outside captured objects; mark symlinks unsupported | Traversal/symlink fixtures |
| Clone credential leak | Approved Git credential transport; no token URL/argv/job record; sanitized errors | Process/log/job capture |
| Code execution during indexing | Disable Git hooks/filters and project scripts; isolate optional advanced indexers | Fixture script never runs in baseline |
| Graph cycle or fan-out DoS | Visited states, cycle detection and total budgets | Cycle/hub test terminates and reports truncation |
| Hallucinated technical path | Evidence IDs, edge types, claim check, explicit unresolved gaps | Evaluation cases reject unsupported consumer/transaction claims |
| Hosted repo access escalation | Authenticated user, repo ACL, origin protection | Negative authorization tests before server profile ships |

Secret detection is fallible. Strict no-cloud-code-egress is an enforced network/provider mode, not a claim that scanning finds all secrets. Cloud opt-in sends filtered proprietary context, so the UI and README must describe that data flow. Provider retention claims require current provider documentation and account configuration.

During Phase 0, fixture markers are deliberately synthetic and are not real credentials. Do not place real secrets in testdata. Later logs should contain IDs, counts, timings, and error categories, never code, credentials, full prompts, or environment dumps.
