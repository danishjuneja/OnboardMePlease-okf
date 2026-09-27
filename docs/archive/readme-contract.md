> Historical document. Current behavior is described in [the architecture guide](../architecture.md).

# Repository Explorer — README and setup contract

Status: requirements for the future application's README, not installation instructions for an existing release. Proposed command names below must be implemented and verified before being published as runnable instructions. Never fill unknown release URLs or version numbers with invented values.

## 1. README opening

Explain the product in one paragraph: provide a public GitHub URL, receive a technical onboarding overview, and investigate implementation with cited source evidence. Include a short example answer shape and a diagram of local service, database, repository storage and optional inference providers.

State what works without a model: inventory, structural analysis where supported, exact/lexical search and source navigation. Generated overview/chat need a configured provider. Strict local mode is the default; cloud access is a separate explicit choice. No promise that every language has identical analysis depth or that static analysis proves runtime behavior.

## 2. Tested support matrix

Publish exact tested OS versions/architectures, Docker/Compose, Git, PostgreSQL/pgvector and native package requirements for each release. Pin supported application versions and link checksums.

| System | Primary documented route | Required notes |
|---|---|---|
| Linux x86_64 / ARM64 | Linux containers or tested native package | Engine/Compose versions, volume permissions, ports |
| macOS Intel / Apple Silicon | Linux containers or tested native package | Docker environment, architecture selection, file sharing |
| Windows x86_64 | Docker with WSL2/Linux containers or tested native package | PowerShell commands, Git, permissions |
| Other combinations | Only if actually tested | Mark experimental/unsupported explicitly |

Do not claim support because cross-compilation succeeded. Native parsing and real repository ingestion must run on each advertised target. Clarify that Compose file-based secrets are for Linux containers; Windows uses Linux containers in this setup.

## 3. Fastest installation path

Provide complete, copy-pasteable sequences separately for POSIX shells and PowerShell. Each sequence must:

1. Obtain a real tagged release or clone the actual application repository.
2. Verify release checksum where applicable.
3. Create configuration/data/secret locations without writing real secret values into command history.
4. Select strict local or explicit cloud mode, with local mode preselected.
5. Configure PostgreSQL credentials and optional provider credentials through files or supported secret integration.
6. Start the application/database with the published Compose profile or native commands.
7. Wait for database readiness and apply migrations safely.
8. Run a doctor/health command and identify the actual local URL/port.
9. Ingest a verified public GitHub example and show expected overview/coverage behavior.
10. Describe how to stop/restart without deleting data.

Do not require API keys for a boot smoke test. Provide a mock provider profile for developers; it must visibly label generated answers as mock outputs and cannot be enabled accidentally in production.

## 4. Native installation

Document supported binaries, verification, extraction, PATH configuration, Git requirement, parser bundle location and PostgreSQL connectivity. Explain which dependencies are bundled and which are external. End users of release artifacts should not need Node merely to serve the prebuilt UI.

For development from source, list the exact supported Go, Node/package-manager, C compiler/native parser and database requirements. Include frontend build/embedding, Go build, migrations, dev servers, test commands and generated code steps. Do not rely on shell scripts alone; provide PowerShell equivalents or a cross-platform setup command.

## 5. Configuration reference

Ship a validated example configuration and a generated reference table. Proposed names, to finalize in M1:

| Setting | Purpose | Required default behavior |
|---|---|---|
| APP_LISTEN_ADDR | Application bind address | Loopback in local mode |
| APP_DATA_DIR | Snapshots and application data | Configurable platform-appropriate location |
| DATABASE_URL_FILE | Database connection secret file | Backend only; never returned by diagnostics |
| MODEL_MODE | strict_local or cloud_opt_in | strict_local |
| LOCAL_GENERATION_URL | Local generation service | No automatic external fallback |
| LOCAL_EMBEDDING_URL | Local embedding service | Validate model/dimensions |
| OPENAI_API_KEY_FILE | Cloud authentication | Only read when cloud mode is authorized |
| EMBEDDING_MODEL | Selected embedding model | Provider-specific, namespaced index |
| GENERATION_MODEL | Selected generation model | Provider-specific, explicit |
| GIT_CREDENTIAL_MODE | Approved Git authentication method if private repositories are added later | No credentials in URLs/argv/job payloads |
| TELEMETRY_ENABLED | Operational telemetry | false |
| GRAPH_MAX_HOPS / NODES | Traversal budget | Document actual implementation defaults |
| QUERY_MAX_ROUNDS / TOKENS | Total investigation budget | Enforced across subqueries |

Explain precedence among flags/config/files, which settings require restart, and how invalid combinations fail. Do not print effective secret values. A doctor command may say a secret is configured/readable, never reveal it.

## 6. Provider setup and privacy modes

### Strict local

Document at least one end-to-end tested local generation/embedding configuration with exact model IDs, capabilities and measured minimum/recommended resources. The chosen local runtime and weights require a milestone evaluation; until validated, do not advertise unverified hardware or model compatibility. The adapter protocol alone does not prove a local model can follow the required schemas.

Explain container-to-host connectivity without opening an inference service publicly. Strict offline deployment requires local endpoints and network enforcement; a configuration flag alone cannot guarantee isolation from a misconfigured local service.

Without configured local models, label generated features unavailable. Allow structural browsing and search. Explain why changing embedding models requires reindexing.

### Cloud opt-in

Document provider setup using protected secret files and repository-scoped consent. Identify data categories sent: sanitized question, approved source excerpts/metadata and derived context. Explain that source code remains potentially confidential even after secret scanning.

Link current provider data controls and avoid claims of universal zero retention. If supported by the chosen API, use non-persistent request settings deliberately; distinguish those settings from provider abuse-monitoring or account-specific retention policies.

Explain that a cloud provider receives its own API credential as authentication, but credentials are never sent as prompt content, embedded code, exported documentation, or browser configuration.

## 7. Adding repositories

Document public GitHub examples with verified URLs. Private GitHub access, local paths, worktrees, and freshly initialized repositories are outside the current input contract. Never include actual tokens or user-specific absolute paths.

Explain branch/tag selection and the captured commit OID. State what happens to ignored files, submodules, LFS pointers, very large files, inaccessible paths and excluded secrets.

For container installs, explain that the backend clones public GitHub repositories into its private volume and needs outbound GitHub access. The UI never supplies a host filesystem path.

Document cloning/storage locations, per-repository policy, reindexing, cancellation/resumption and deletion. Do not suggest running the analyzed application or build scripts to complete baseline indexing.

## 8. Using the application

Explain the overview and its coverage status, component/flow navigation, source citations, technical questions and follow-ups, profile selection, unresolved edges, cycles, search fallbacks and partial analysis. Include examples for a web application, a CLI and a library.

Describe index progress and distinguish failed, partial, stale and ready states. Explain that a completed job does not guarantee complete semantic understanding. Show how to inspect excluded/unsupported files and enable a supported adapter.

## 9. Prompts and knowledge artifacts

Document every packaged prompt ID, input/output schema, allowed tools and version. Point to the checked-in templates rather than asking users to construct their own system prompts.

Explain how administrators can override prompts, how overrides are validated/versioned, how evaluation runs, and how to reset defaults. Repository content must not be able to supply prompt overrides.

Document OKF export location, schema/spec version, source provenance, generated versus reviewed claims, stale-document behavior, redaction, local citations and export limitations. Describe how human corrections survive regeneration. Warn before exporting a bundle containing proprietary sanitized code; absence of credentials does not make code public.

## 10. Self-hosted operation

Provide a separate guide linked prominently from the README for server mode. Include authentication, repository authorization, TLS/reverse proxy, origin protection, persistent volumes, approved mounts, model egress policy, resource limits, health/readiness checks and backup/restore.

Do not expose PostgreSQL directly in the default Compose network. Expose the local application on loopback; remote access is an explicit authenticated profile. Explain secret rotation and filesystem permissions; Compose mounts alone do not encrypt host secrets.

Document graceful worker shutdown, job resumption, queue backlog, token/API budgets, sanitized diagnostics and rate-limit behavior. Provide tested upgrade/migration steps and realistic rollback constraints. Backup/restore tests must cover metadata, snapshots and derived evidence consistency.

## 11. Troubleshooting

Include symptoms, diagnostics and fixes for: Git unavailable, path not mounted, permission denied, invalid/unborn HEAD, Docker not running, unsupported architecture, database readiness/migrations, missing pgvector, parser failure, local model unavailable, invalid model response, provider rate limits, secret scan failure, blocked outbound calls, partial graphs, cycle/traversal limits and stale pages.

Diagnostic instructions must not ask users to paste secrets, full environments, raw provider payloads or private source into public issues. Supply a sanitized support-bundle command and document its contents.

## 12. Verification and contribution

List unit/integration/UI/evaluation/installation test commands and how to run with mock providers. Real-model tests must be opt-in. Explain how to add a grammar, semantic adapter, framework adapter and prompt with required fixtures.

Document the capability matrix and evaluation methodology. Provide a security reporting route in SECURITY.md, license information and dependency/model licensing requirements. Do not invent a contact address before the project owner supplies one.

## 13. README acceptance checklist

- A clean supported Windows, macOS and Linux environment can follow its documented route.
- Every copy-paste command was executed in that environment or an equivalent documented CI test.
- A no-key smoke test works and clearly identifies mock outputs when used.
- A local-provider setup and an authorized cloud setup are each tested separately.
- No absolute developer paths, real credentials, floating release URLs or fabricated versions occur.
- Exact configuration names, ports, volumes, profile names and CLI commands match the application.
- The first example repository produces expected coverage, sources and overview behavior.
- Restart, cancellation, reindexing, backup/restore and version upgrade are exercised.
- Source privacy, retention, deletion, GitHub URL semantics and support limits are explicit.
- Links, code fences and examples are checked in CI; setup tests gate release documentation.

## Documentation deliverables

README.md; docs/installation.md; docs/configuration.md; docs/privacy.md; docs/local-models.md;
docs/deployment.md; docs/operations.md; docs/architecture.md; docs/prompts.md;
docs/analysis-capabilities.md; docs/evaluation.md; SECURITY.md; CONTRIBUTING.md;
validated configuration examples; POSIX and PowerShell smoke-test scripts.
