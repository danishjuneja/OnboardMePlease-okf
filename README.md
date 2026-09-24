# OnboardMePlease 

This project is an attempt to learn Google's OKF and also solve a very common problem most of us face - understanding enterprise level monolithic repos which usually have a ton of different working components. I have spend my fair share of time working with huge monoliths and don't particularly enjoy being clueless about the codebase. 

The basic idea is - Use OKF to group similar files and generate a knowledge base of the repository. Whenever a user wants to get started, or even start working on something they haven't worked on before, instead of pinging codex with their specific problem statement, they can get an overall idea of all the components related to their use case. 

A traditional semantic RAG obviously fails for larger lookups, or cross-module tracing. I also explored Graph-RAG but that again has a lot of compute for indexation. Synthetic docs using OKF seemed like a good idea, as if focusses more on grouping its findings on the domain/business capabilities. 

## Implementation status

The project is being built phase by phase. Phase 0 defines the technical behavior, data and API contracts, analysis limits, privacy boundaries, and synthetic evaluation cases. Phase 1 adds a locally runnable GitHub repository-capture service and UI shell. Phase 2 adds source evidence, search, capability reporting, and bounded call graphs.

- [Phase 0 contracts and exit gate](docs/phase-0.md)
- [Implementation plan](implementation-plan.md)
- [Prompt library](docs/prompt-library.md)
- [Deployment and README requirements](docs/readme-contract.md)
- [Phase 2 scope and verification](docs/phase-2.md)

The planned first release accepts a public GitHub repository URL and will produce a technical onboarding overview before interactive, source-backed questions. Strict local model processing is the default design. Cloning a public repository still requires network access to GitHub; cloud model calls require explicit opt-in and a repository policy.

## Run locally (Phase 2)

The service captures an immutable, scanned repository snapshot, then indexes approved files as citable source chunks. Exact and lexical search, source viewing, Go syntax extraction, capability reporting, and bounded call-graph navigation run without a model or API key. Optional SCIP import adds semantic references only where the index's embedded file text matches the captured commit. Optional vector search requires a separately configured embedding provider. The technical overview, OKF bundle, and interactive answers are still planned for later phases. Baseline prompts for the implementation agent, OKF generation, and interactive answers are in the [prompt library](docs/prompt-library.md); packaging them as a versioned runtime registry remains a later phase.

A capture request creates a repository row, a queued snapshot, and a durable River job. The worker clones one public GitHub commit, inventories and scans its files, writes approved content and a manifest to private storage, then indexes text in PostgreSQL. The UI reads job state and coverage, searches source chunks, opens cited lines, and explores supported Go call links. `analyzed` means searchable text, not complete technical understanding. `pending` means capture has not yet been indexed; `completed` means that job finished, including its index step. Each search is scoped to one repository snapshot. Graph traversal uses visited nodes and fixed limits, and reports truncation.

The easiest route is Docker Desktop with Linux containers and WSL2 on Windows, or Docker Engine with Compose on Linux and macOS. Docker must be running. The app is published only at `127.0.0.1:8765`; this is a single-user local setup, not a remote server configuration. Docker images are built from the checked-in Dockerfile. GitHub URLs are cloned by the backend into its private data volume. Use GitHub repositories that are public and do not require credentials. The service does not accept local repository paths or mount host repositories.

From the project root, create the Compose database password file. It is ignored by Git and by the Docker build context. On Linux/macOS:

```sh
umask 077
mkdir -p secrets
openssl rand -base64 32 > secrets/postgres_password.txt
docker compose up --build -d
docker compose ps
```

On Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force secrets | Out-Null
$password = [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
[System.IO.File]::WriteAllText((Join-Path (Get-Location) 'secrets/postgres_password.txt'), $password)
Remove-Variable password
docker compose up --build -d
docker compose ps
```

Open [http://127.0.0.1:8765](http://127.0.0.1:8765). The `doctor` command checks Git, the writable data directory, and pgvector; `docker compose exec app /app/onboardmeplease doctor` runs it in the container. `docker compose logs app` shows startup or migration failures without dumping repository content or passwords. To stop and resume while preserving jobs and snapshots, use `docker compose stop` then `docker compose start`. Do not use `docker compose down -v` unless you intend to delete both persistent volumes.

For a no-key smoke test, enter `https://github.com/octocat/Hello-World` in the UI. The completed job should show a nonzero inventory with readable source files marked `analyzed`. Open the snapshot from Recent repositories and search for `Hello`; the result opens its source lines. You can select a branch or tag, or leave the ref empty to capture the default branch's current commit. Only credential-free `https://github.com/owner/repo` URLs are accepted.

On PowerShell, `./scripts/smoke_phase1.ps1` captures and indexes that public repository through the API. To verify durability, run `docker compose restart database` followed by `./scripts/check_phase1_persistence.ps1`. The scripts save only job IDs, snapshot IDs, hashes, and coverage counts under the ignored `work` directory.

For native development, install Go 1.27.1, Node 24.21.0, Git, and PostgreSQL 17 with pgvector 0.8.6. Build the embedded UI first, then the Go binary:

```sh
cd web && npm ci && npm run build && cd ..
go build -o onboardmeplease ./cmd/onboardmeplease
```

Set `DATABASE_URL` to a PostgreSQL connection string. Prefer `DATABASE_URL_FILE` or `DATABASE_PASSWORD_FILE` for protected credentials. `APP_DATA_DIR` chooses snapshot storage; the platform user config directory is used by default. Run `./onboardmeplease doctor`, `./onboardmeplease migrate`, then `./onboardmeplease serve` on POSIX systems, or `./onboardmeplease.exe` with the same subcommands on Windows. The native app binds `127.0.0.1:8765` by default. A native build is source-buildable; release binaries and an OS verification matrix are not published yet.

The snapshot inventory reads files from one resolved Git commit, so later branch changes do not alter a captured snapshot. Secret-like files and detected secret content are excluded; symlinks, submodules, LFS pointers, binaries, and files larger than 2 MiB are listed as unsupported rather than silently analyzed. Capture never runs repository build scripts or hooks. Secret scanning is conservative and cannot prove arbitrary text is secret-free. The default installation sends no repository content to a model.

Large GitHub clones and scans can take time. `APP_CAPTURE_TIMEOUT` defaults to `2h` and accepts `1m` through `24h`; set it in the ignored `.env` file and recreate the app container if needed.

Run `go test ./...` for backend and snapshot checks, `cd web && npm run build` for the UI, and `python scripts/check_phase0.py` for the design contracts (Python 3.11+ and PyYAML 6.0.3). [Phase 2 scope and verification](docs/phase-2.md) records what is implemented and what remains unverified. For secrets, report a sanitized reproduction; do not paste real credentials or private source into an issue.

Optional embeddings require an explicit provider and an explicit indexing action in the UI. The default Compose profile has none. For a native service with a loopback embedding endpoint, set `LOCAL_EMBEDDING_URL`, `LOCAL_EMBEDDING_MODEL`, and `LOCAL_EMBEDDING_DIMENSIONS` in the service environment before starting it; the endpoint must accept the OpenAI-compatible embeddings request shape and return the configured number of floats. A provider error never selects cloud as a fallback. The local endpoint must be reachable from the backend process; a host loopback address is not a container loopback address.

Cloud embeddings use `text-embedding-3-small` with 1,536 dimensions and are opt-in at both installation and repository level. To enable the Compose overlay, place your key in the Git-ignored `secrets/openai_api_key.txt` using a protected editor, then start with `docker compose -f compose.yaml -f compose.cloud.yaml up --build -d`. Choose **Cloud opt-in** for a repository and then explicitly select **Index semantic vectors**. This sends approved source chunks to OpenAI's embeddings endpoint. Do not put a key in `.env`, the URL, browser code, or a command argument. Provider data controls can change; see the [official OpenAI documentation](https://developers.openai.com/api/docs/guides/your-data). No cloud call is required for capture, lexical search, SCIP import, or graph navigation.
