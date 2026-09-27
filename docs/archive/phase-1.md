> Historical document. Current behavior is described in [the architecture guide](../architecture.md).

# Phase 1 implementation and verification

This records the earlier foundation milestone. [Phase 2](phase-2.md) describes the current evidence and search implementation.

Phase 1 is the foundation service. It accepts public GitHub URLs, validates the source, queues a durable River job, captures an immutable commit snapshot, stores artifact coverage in PostgreSQL, and reports status through a local UI and API. New local repository submissions are rejected. It does not claim to understand repository components or business flows. Those require later parsing, relationship, retrieval, and evidence phases.

The service uses Go `net/http`, pgx, PostgreSQL 17 with pgvector, River, and an embedded React/Vite frontend. The release image is built by `Dockerfile`; `compose.yaml` runs the app and database with persistent volumes. The database password is read from a Compose secret file. The app container accepts traffic on its container interface, while Compose publishes the port only on host loopback. Native mode binds loopback directly. The backend clones public GitHub repositories into private storage; no host repository mount is required. The local session cookie, strict same-origin checks, and CSRF token protect mutating API calls. There is no authenticated remote-server mode.

Snapshot capture keeps a manifest hash and file hashes, source type, commit OID, an artifact status for every inventoried path, and a private copy of approved content. The worker is idempotent for an already published snapshot ID. It never executes checked-in scripts or Git hooks. It resolves a single GitHub commit before reading files. Submodules and symlinks are recorded as unsupported. The scanner excludes sensitive paths and known secret shapes before storing content. This scanner is deliberately conservative and not a guarantee that arbitrary secrets are recognized. All model adapters must pass the centralized privacy authorizer; no live cloud or local adapter is connected in this phase. Mock generation and embedding implementations are deterministic test facilities only and are not presented as repository answers. pgvector is installed, but there is no chunk, embedding, or retrieval pipeline yet.

The setup commands in the README are source-install instructions. Published release images and binaries, a Windows/macOS/Linux CI matrix, backup/restore, cancellation and reindex endpoints, fully isolated parser workers, production secret scanning, and remote authenticated hosting are not yet delivered. Later-phase OpenAPI paths describe the target API, not Phase 1 capabilities.

Verification commands:

```sh
go test ./...
cd web && npm ci && npm run build
cd .. && python scripts/check_phase0.py
docker compose config
docker compose up --build -d
docker compose exec app /app/onboardmeplease doctor
```

The last two commands require a running Docker Linux backend and a local `secrets/postgres_password.txt` generated as described in the README. An end-to-end Compose smoke test must confirm job persistence across database restart before Phase 1 is called fully verified on a platform.

On 2026-09-24, the frontend production build, Go tests, Go vet, Phase 0 checker, Compose configuration validation, native `doctor`, and Linux amd64/arm64 cross-builds passed on Windows. After Docker Desktop's Linux engine became available, the app image built and the Compose stack booted on Windows x86_64 with Linux amd64 containers. The local health check, embedded UI, migrations, and in-container `doctor` passed. Earlier tests of local Git capture predate its removal; old snapshot rows are retained for history but new local submissions are blocked. On 2026-09-25, the rebuilt GitHub-only Compose service captured `https://github.com/octocat/Hello-World` as a ready commit snapshot with one pending artifact. Its completed job, manifest hash, and inventory survived a PostgreSQL restart; both old local API request shapes returned HTTP 400, and a retired local job was cancelled. Go tests, vet, frontend build, Phase 0 checks, Compose configuration, and in-container `doctor` passed again. Linux, macOS, and arm64 runtime tests remain to be run. Cross-building alone does not establish runtime support on those platforms.
