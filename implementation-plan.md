# Repository Explorer — implementation plan

Status: phased implementation in progress. Phase 0 contracts, the Phase 1 foundation, and a Phase 2 evidence/search implementation are present in this working tree; overview and answer phases remain planned. See [Phase 2 status](docs/phase-2.md).

This plan incorporates the agreed architecture and four release requirements: portable deployment, a complete versioned prompt library, protection of secrets, and reproducible setup documentation. “Repository Explorer” is a working label, not a final product name.

Companion documents:

- [Prompt library](docs/prompt-library.md): implementation-agent handoff and runtime prompt templates.
- [README contract](docs/readme-contract.md): required installation instructions and documentation verification.

## 1. Product contract

A user supplies a public GitHub repository URL. The application produces a technical onboarding overview, then supports evidence-backed questions about the repository. Local repository paths are outside the current product scope.

The overview covers repository purpose, runtime units, components, startup, deployment profiles, configuration, data stores, external dependencies, technical execution flows, suggested exploration entry points, and analysis coverage. It adapts to web applications, services, CLIs, libraries, and mixed repositories. It never assumes a UI or deployable service exists.

Answers connect consumer actions to implementation: entry points, handlers, validation, calls, state changes, transactions, events, workers, responses, error handling, and tests. Every repository-specific factual claim must carry evidence or be clearly presented as an inference or unresolved question.

An exhaustive inventory precedes the overview. Top-k retrieval alone cannot establish a repository-wide overview. Every inventoried artifact is accounted for as analyzed, excluded with reason, unsupported, failed, or not yet analyzed. Separate discovered/in-scope coverage from excluded files so exclusions cannot inflate completeness.

The product is language-independent at the data model and retrieval layers, with language- and framework-aware analysis adapters. Unsupported code remains searchable. “Analysis complete” means the scheduled analysis finished; it does not mean all runtime behavior is known.

## 2. Decisions

| Concern | Decision |
|---|---|
| Backend | Go, standard net/http, REST, OpenAPI contract, server-sent events |
| Repository access | Native Git CLI behind a restricted repository-access interface |
| Parsing | Tree-sitter Go bindings, packaged grammars, isolated worker processes |
| Semantic analysis | SCIP ingestion and optional language/framework adapters |
| Persistence | PostgreSQL, pgvector, full-text and exact identifier indexes |
| Database access | pgx and sqlc; versioned SQL migrations |
| Durable jobs | River; idempotent stages, retries, cancellation, resumable progress |
| Graph | Typed node/edge tables in PostgreSQL; bounded traversal in Go |
| Retrieval | Exact identifiers + lexical + vector search; rank fusion and evidence expansion |
| Cloud embeddings | text-embedding-3-small, initially 1,536 dimensions |
| Cloud synthesis | Responses API, configurable gpt-5.4 baseline; no model-specific logic in domain packages |
| Local models | Provider interface for local generation/embedding endpoints; validated capability handshake |
| Knowledge | OKF v0.2 bundles, generated from validated structured claims |
| UI | React, TypeScript, Vite, Tailwind, shadcn/ui, TanStack Query |
| Diagrams/source | React Flow + ELK; Monaco read-only source viewer |
| Packaging | Container images and Compose first; tested native Go distributions with embedded UI |
| Verification | Go tests, Vitest, Playwright, fixed repository fixtures and answer evaluations |

Pin actual toolchain, dependency, grammar, model snapshot where available, and image versions during the foundation milestone. Commit lockfiles; never use floating latest tags in published installation examples. Pin the OKF specification revision used by the validator. A provider/model change is a versioned configuration change, not an invisible upgrade.

## 3. Deployment and portability

### Supported installation paths

1. **Container installation:** Linux application containers plus PostgreSQL/pgvector, using Docker Compose. Publish linux/amd64 and linux/arm64 images only after both pass tests. Windows and macOS run these Linux containers using an appropriate Docker environment; Windows instructions explicitly cover WSL2/Linux containers.
2. **Native application installation:** platform-specific Go release binary with embedded frontend and packaged parser support; Git on the host and an existing or containerized PostgreSQL instance. Validate Linux, macOS, and Windows builds individually. Native architecture combinations not tested must be labeled unsupported, not presumed working.
3. **Self-hosted server installation:** same application and worker images, persistent volumes, and authenticated HTTPS access. This profile must not ship enabled until authentication and repository authorization tests pass.

“Any system” means documented prerequisites and a tested support matrix for common systems, not a claim that arbitrary operating systems or architectures work.

Tree-sitter includes native code. Build packages on supported runners, test native libraries and bundled grammars, and publish checksums. Do not assume CGO_ENABLED=0 produces a usable universal analyzer.

### Runtime topology

- A Go application serves static UI, API, and event streams. Local mode binds loopback by default.
- A worker executes durable jobs; a small installation may colocate the worker with the application. Both modes use the same job contracts.
- PostgreSQL holds sanitized evidence, searchable metadata, vectors, graph edges, job state, and chat records.
- A protected filesystem volume holds repository snapshots and generated bundles. Original repository material is not automatically included in exports or backups of sanitized application data.
- Optional local model endpoints serve private inference. A hosted endpoint is enabled only by explicit policy.
- A self-hosted server uses authentication, per-repository authorization, TLS, and managed backups. No unauthenticated remote-bind configuration is considered production-ready.

Repository input is a public GitHub URL, independent of the browser or backend operating system. Local filesystem paths are not accepted by the service.

Use portable filesystem APIs, configurable data directories, normalized internal repository-relative paths, and no developer-specific absolute paths. Test spaces, Unicode, CRLF, case sensitivity, long paths, symlinks, and permissions. Keep captured content inside the configured private data directory.

## 4. Ingestion and snapshot semantics

1. Validate public GitHub HTTPS URLs. Reject embedded URL credentials and unsupported remote schemes. GitHub enterprise support, if offered, requires configured host allowlists.
2. Clone without credentials or checked-in hook execution, resolve a requested ref or the default branch to a commit OID, and capture committed objects only.
3. Never silently combine files from different revisions. Record the resolved commit and immutable manifest so later branch changes cannot alter the snapshot.
4. Inventory paths and file types. Record large files, inaccessible files, binaries, missing submodules, Git LFS pointers, and exclusions. No automatic submodule or LFS downloads in the baseline.
5. Exclude credential stores, environment secrets, private keys, Git internals, dependencies, generated artifacts, and binaries from model processing by default. Preserve useful non-secret configuration key names and clearly label masked values. Allow a reviewed inclusion policy without disabling the secret boundary.
6. Scan raw content locally before constructing embeddings, model requests, searchable content, or exports. Parser workers may analyze original source locally, but emitted facts and snippets pass through sanitization.
7. Store file/symbol evidence with snapshot ID, path, source ranges, content hash, extractor version, and resolution method.
8. Publish a new ready snapshot atomically. Partial snapshots remain explicitly partial; failed re-indexing never replaces the last ready snapshot silently.

Clone or read repositories without executing hooks, filters, project scripts, or build commands. Restrict inherited Git configuration, external helpers, transport schemes, filesystem escapes, and environment variables. Credential helpers used for private Git access are deliberately scoped. Deeper build-dependent indexing runs only in a separately configured isolated analysis mode.

## 5. Analysis model and coverage

Core entities: Repository, Snapshot, Artifact, Symbol, EntryPoint, RuntimeUnit, DataStore, MessageChannel, ExternalDependency, Component, Flow, Evidence, Claim, KnowledgeDocument, Conversation, AnalysisJob.

Core relations: defines, references, calls, implements, routes_to, reads, writes, publishes, subscribes, configured_by, supported_by, derived_from.

Namespace symbol identity by repository, snapshot, language, containing scope, and extractor identity. Identical names do not identify identical symbols.

Keep extracted structural facts separate from model-proposed groupings and interpretations. Every relation records its evidence, extractor, resolution status, and applicable configuration scope. Configuration alternatives cannot be merged into one asserted runtime path.

Implement capability levels:

- Text: readable, searchable, citable.
- Syntax: structural chunks and declarations.
- Semantic: resolved symbols/references where an adapter supports them.
- Framework: routes, injection, persistence and message wiring.
- Runtime: optional observed traces tied to environment and scenario; outside the first release.

Ship a capability matrix by language and framework. Start with tested grammars/extractors for JavaScript/TypeScript, Go, Python, and Java, extend with independently tested packs, and retain text fallback for other readable languages. This is broad ingestion with explicit depth differences, not equal semantic support for every language. SCIP import should accept supported externally produced indexes rather than require the application to reproduce every compiler toolchain.

Discover components through declarations, references, manifests, registration, configuration, and entry points. Directories are hints only. Preserve disconnected nodes and unclassified artifacts. The inventory is deterministic; an LLM may propose component labels but cannot invent artifact IDs or silently drop unclassified files.

For deployment explanations, inspect launch scripts, container entry points, Compose/Kubernetes files, manifests, environment references, initialization, migrations, and worker registrations. Describe behavior for each discovered profile and flag missing external configuration. Static analysis does not establish that deployment succeeded.

## 6. Retrieval, graph limits, and answer verification

All retrieval is scoped to an authorized repository and explicit snapshot before context construction. Apply identical authorization to source APIs, graph APIs, chat, caches, OKF exports, and event streams.

Use exact lookup, code-aware lexical search, and semantic search. Tokenize identifiers both intact and split on camelCase/snake_case; ordinary prose full-text search alone is insufficient for code. Combine candidate ranks through reciprocal rank fusion; add a measured reranking stage if evaluation warrants it.

Store embedding provider, model, dimensions, and content-policy version. Never compare incompatible embedding spaces. A provider/model change requires a distinct index or re-embedding. Do not fall back to cloud when a local provider fails.

Graph expansion is application-controlled, not an unrestricted agent loop:

- Allowed edge types and directions per query.
- Visited traversal states plus per-path cycle detection; preserve alternate predecessor paths without repeatedly expanding equivalent states.
- Strongly connected component grouping for diagrams, with access to internal edges.
- Initial configurable limits: 6 hops, 200 expanded nodes, 20 outgoing candidates per expansion, 3 follow-up retrieval rounds, plus wall-clock and token budgets.
- A total request budget shared across subqueries so query decomposition cannot bypass limits.
- Explicit truncation/coverage indicators when limits bind; never present bounded traversal as proof of completeness.

Read authoritative source snippets after candidate retrieval. Validate source IDs/ranges and graph path consistency deterministically. Run a separate claim-support assessment; downgrade or remove unsupported statements. LLM verification is an additional check, not a proof. Links prove where evidence is, not that a claim follows from it.

Conversation follow-ups resolve references to prior discussion but re-ground repository facts against the selected snapshot. Previous assistant text is not independent evidence. Changing snapshot invalidates or labels old context.

## 7. Prompt and OKF requirements

The companion prompt library supplies baseline text for the implementation agent, shared runtime rules, component discovery, startup analysis, flow analysis, overview composition, query planning, evidence assessment, interactive answers, OKF generation, revisions, and schema repair.

During implementation, store each template under prompts/ with a manifest containing ID, semantic version, input/output schema, allowed tools, budgets, examples, and checksum. Embed the default registry in release binaries; validate all referenced templates at startup. Do not scatter hidden prompt strings through handlers.

Use typed JSON inputs and outputs. Application code resolves evidence IDs to citations. Treat retrieved files, documents, comments, prior messages, and tool results as untrusted data. Sanitization occurs before template rendering. Never interpolate raw source into a system instruction or grant tools based on repository instructions.

The model returns structured OKF document content. A deterministic renderer writes Markdown/YAML, stable source IDs and footnotes. Validate OKF v0.2, internal links, provenance, source ranges, paths, and secret policy before export. Do not fabricate generated/verified timestamps or human-review records; trusted application code supplies those fields.

Maintain a source-to-claim-to-document dependency index. On change, identify affected files, symbols, configuration, relations and dependents; conservatively mark unknown impacts stale. Publish regenerated documents as a new version and preserve reviewer corrections. Human-review status does not automatically carry over to materially changed generated claims.

## 8. Secret handling and data egress

### Policy distinction

Application credentials are used only for authentication to their intended service. They are never repository evidence or model context. Repository secrets must not enter model requests, embeddings, chat history, logs, UI snippets, OKF, exports, support bundles, or release artifacts.

Secret detection cannot guarantee recognition of every arbitrary secret. The strict no-external-code-egress guarantee comes from enforced network policy/local processing, not from a scanner or a prompt.

### Modes

- **Strict local (default):** external model calls and telemetry are disabled. Search and structural analysis work without model credentials. Chat/semantic search require configured local providers; absent providers result in an explicit unavailable capability, never automatic cloud fallback. For the strictest offline profile, inference services run on loopback or an isolated container network with outbound access denied.
- **Cloud opt-in:** repository-scoped authorization allows sanitized code/context to the configured provider. Explain that private code can still leave the machine and that scanning is risk reduction, not proof that arbitrary secrets are absent. Pin allowed provider endpoints and send the minimum necessary evidence. Separate model egress from GitHub cloning and package download/bootstrap traffic.

### Required controls

1. Use runtime secret files or supported secret-manager integrations. Support *_FILE configuration. .env.example and sample configs contain placeholders only. Runtime secret paths are excluded from Git and image build context.
2. For Compose, mount secrets only into services requiring them; application/worker model credentials do not go to frontend assets, parsers, or optional indexers. Compose secret files need host filesystem protection; file mounting is not encryption at rest.
3. Never use Docker ARG/ENV to bake credentials into layers. Use BuildKit secret mounts only if a build genuinely needs authentication. Build application images without user repositories or runtime secrets.
4. Use local secret scanning (Gitleaks plus configured detectors) and a policy layer for file exclusions and masking. Scanner errors block model egress for affected content. Do not persist unredacted scanner reports or log subprocess output containing a finding.
5. Scan user questions and attachments before persistence/provider dispatch, as well as repository content. Keep original user text out of ordinary logs. Validate and scan generated responses and export artifacts before release to the UI.
6. Preserve source line correspondence when masking; display placeholders. Restrict original snapshot storage with OS permissions and deployment-level disk encryption where needed. Public bundles include sanitized evidence only; they do not copy raw repositories.
7. All provider requests pass through a single egress gateway enforcing mode, authorization, allowed endpoints, request size, sanitization status, and credential separation. Disable content logging in SDKs, proxies, tracing, and crash diagnostics.
8. Telemetry is off by default. Operational logs contain IDs, counts, durations, error categories, and policy decisions, not prompts, code, credentials, environment dumps, or raw remote URLs.
9. Use safe Git authentication without embedding tokens in clone URLs, command-line arguments, persistent remotes, error output, or job payloads. Secrets are resolved at execution time, not serialized into queues.
10. Local UI requires a local session and origin/CSRF protections; reject arbitrary cross-origin access. Server mode requires authenticated identity and per-repository authorization. Do not expose arbitrary host paths to remote users.
11. Document retention and deletion for clones, sanitized evidence, vectors, chat, OKF, job payloads, and backups. A repository deletion invalidates derived caches and access immediately; disclose backup expiry separately.
12. Never execute instructions from indexed content. Optional build/indexer execution has no model/API credentials and requires isolation, resource limits, and separately controlled network access.

No request to obtain real keys or private repositories is needed to implement or test this plan. Use mock providers and synthetic fixtures by default.

## 9. UI requirements

- Add repository: public GitHub URL, optional branch/tag, privacy mode, readable scope preview.
- Index progress: inventory, parsing, relationships, embeddings, explanations; partial failures and resumption visible.
- Overview is the landing view, followed by Components, Flows, Deployment, Data, and Coverage.
- Main explanation/graph panel; read-only source drawer; persistent question panel with selected scope.
- Evidence badges distinguish extracted fact, supported interpretation, and unresolved connection. Avoid uncalibrated confidence percentages.
- Diagram and list alternatives; lazy-expand large graphs; accessible keyboard navigation.
- Clearly label snapshot, local changes, stale pages, truncated exploration, and unavailable capabilities.
- Chat must remain usable without drawing a whole-repository graph. Source citations work for local snapshots without publishing code to GitHub.
- Export validated OKF and the technical overview; show export scope and sanitize before writing.

## 10. Proposed repository layout

```text
cmd/explorer/             application, worker, doctor and migration commands
internal/repository/      Git access, snapshots, inventory
internal/analysis/        common model, parsers, adapters and SCIP import
internal/security/        secret policy, sanitization and egress control
internal/index/           lexical, exact and vector indexes
internal/graph/           traversal, cycles and component grouping
internal/retrieval/       query planning and evidence assembly
internal/providers/       cloud/local adapters and mock providers
internal/knowledge/       claims, OKF rendering and invalidation
internal/chat/            conversations, grounding and answer validation
internal/jobs/            durable stages and cancellation
internal/api/             authenticated API and event streams
prompts/                  manifest, templates, schemas and examples
web/                      React application
db/migrations/            schema migrations
db/queries/               sqlc source queries
deploy/                   Compose, Dockerfile and server examples
scripts/                  portable setup, smoke tests and releases
testdata/                 synthetic repositories and expected findings
evals/                    question sets, graders and regression reports
docs/                     architecture, setup, privacy and operations
README.md
SECURITY.md
CONTRIBUTING.md
.env.example
.gitignore
.dockerignore
```

## 11. Milestones and acceptance gates

### M0 — contracts and evaluation fixtures

Deliver domain schemas, API contract, prompt registry format, supported deployment matrix, threat/data-flow model, grammar capability matrix, and synthetic repositories. Assemble maintainer-reviewed questions before tuning retrieval. Record architecture decisions.

Gate: test cases cover cycles, sparse/scattered directories, duplicate names, mixed languages, missing configuration, incomplete event chains, stale knowledge, secret-bearing code/questions, and unsupported parsers. Schema distinguishes facts from inferences.

### M1 — portable foundation and private ingestion

Deliver Go service, embedded UI shell, PostgreSQL migrations, River workers, mock providers, Docker/Compose build, configuration/doctor commands, secret-file support, local session protection, ingestion and immutable snapshots. Implement privacy gateway before any real provider integration.

Gate: clean installation boots on the tested container profile; public GitHub capture works without credentials; privacy and mock tests require no credentials; database restart preserves jobs; scanner failure cannot produce an outbound model request. Local repository input was removed before Phase 2.

### M2 — evidence, search and graphs

Deliver structural extraction, capability reporting, exact/lexical search, graph storage/traversal, coverage accounting, SCIP import, source viewer and isolated parser failures. Add mock embedding and real provider adapters behind the gateway.

Gate: exact source ranges and stable IDs validate; cycle/hub stress tests terminate; disconnected artifacts remain visible; all query surfaces enforce snapshot/repository boundaries; local provider failure cannot trigger cloud egress.

### M3 — technical overview and OKF

Deliver component/startup/flow prompts and schemas, repository-wide synthesis, deterministic citation and OKF renderers, overview UI, invalidation and retained human corrections.

Gate: no missing inventory entries in coverage accounting; no invented deployment in a library-only fixture; missing runtime configuration stays unresolved; every published factual claim has a valid evidence reference; OKF passes the pinned specification and secret checks.

### M4 — interactive investigation

Deliver planning, bounded evidence expansion, support assessment, structured technical answers, follow-up grounding, stream rendering, partial-answer UX and graph navigation.

Gate: answer suite measures correctness and support, not citation presence alone. Broad questions cannot evade budgets. Chat injection fixtures cannot grant tools, reveal credentials, or switch repositories. Snapshot changes cannot silently reuse stale evidence.

### M5 — deployment, documentation and release

Deliver tested native packages, multi-architecture images, release checksums/SBOM, backup/restore and upgrade procedures, server authentication/authorization profile, README and operations docs, full smoke tests.

Gate: another developer can install on clean documented systems using only the README; all commands are exercised; no developer-specific paths exist; exported images/configs/prompts contain no secrets; restore and upgrade are tested. Do not publish untested platform support claims.

## 12. Evaluation and release criteria

Hard gates:

- Zero known synthetic secret canaries in captured provider payloads, embeddings input, logs, database text records, browser responses, OKF, exports or build artifacts. Passing these tests is bounded evidence, not universal secret detection.
- No external model network requests in strict local tests; bootstrap/Git network access is separately accounted for.
- Every displayed citation resolves within its authorized immutable snapshot.
- No cross-repository disclosure through retrieval, graph/source APIs, chat caches, exports, or SSE.
- All graph/investigation stress tests terminate within configured budgets and report truncation.
- Unsupported languages and parser failures produce capability gaps without aborting unrelated work.
- Every required prompt is present, versioned, schema-validated and exercised; schema repair is bounded.
- Installation smoke tests pass on every platform advertised as supported.

Quality evaluation: compare vector-only, hybrid, graph-expanded, and graph+OKF variants on the same fixed questions and context budgets. Score technical correctness, claim support, relevant-path coverage, abstention, latency and cost. Use human-reviewed answers for release decisions; model grading is supplemental. Set numerical quality/latency targets after measuring the M0 corpus and before tuning; do not invent performance guarantees now.

Freshness evaluation: change a route, handler condition, shared dependency, configuration value and event wiring; verify invalidation and corrected explanations. Also remove a file and add a new cross-cutting dependency. Keep source/prompt/model/extractor versions in evaluation reports.

## 13. Definition of done

The tool can be installed on documented systems, ingest a public GitHub repository, present a technically grounded overview, answer follow-ups with traceable evidence and visible gaps, export valid OKF, update its knowledge after changes, and protect secrets across data flows. The repository includes all prompts, fixtures, setup instructions, configuration examples and operating procedures. Documentation describes limitations as clearly as successful paths.

## References

- [Docker Compose secrets](https://docs.docker.com/compose/how-tos/use-secrets/): per-service secret files; Linux-container support.
- [Docker build secrets](https://docs.docker.com/build/building/secrets/): build-time secret mounts rather than credentials in layers.
- [Gitleaks](https://github.com/gitleaks/gitleaks): local secret detection; application policy must handle omissions and scanner failures.
- [OpenAI data controls](https://developers.openai.com/api/docs/guides/your-data): provider behavior must be documented from current policy; do not promise zero retention by default.
- [OKF specification](https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/main/SPEC.md): document format and provenance conventions.
- [Tree-sitter Go](https://github.com/tree-sitter/go-tree-sitter), [SCIP](https://sourcegraph.com/docs/code-navigation/precise-code-navigation), [pgvector](https://github.com/pgvector/pgvector), [River](https://riverqueue.com/docs): implementation foundations.
