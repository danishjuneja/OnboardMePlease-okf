# OnboardMePlease 

This project is an attempt to learn Google's OKF and also solve a very common problem most of us face - understanding enterprise level monolithic repos which usually have a ton of different working components. I have spend my fair share of time working with huge monoliths and don't particularly enjoy being clueless about the codebase. 

The basic idea is - Use OKF to group similar files and generate a knowledge base of the repository. Whenever a user wants to get started, or even start working on something they haven't worked on before, instead of pinging codex with their specific problem statement, they can get an overall idea of all the components related to their use case. 

A traditional semantic RAG obviously fails for larger lookups, or cross-module tracing. I also explored Graph-RAG but that again has a lot of compute for indexation. Synthetic docs using OKF seemed like a good idea, as if focusses more on grouping its findings on the domain/business capabilities. 

## Implementation status

The architecture rework connects repository capture, source analysis, an initial technical overview, searchable OKF concepts, and technical questions. Go handles the API, River jobs, retrieval, graph traversal, privacy checks and model calls. React/TypeScript/Vite supplies the UI; PostgreSQL with pgvector stores evidence, knowledge, vectors and conversation history.

A public GitHub URL is the only repository input. The service captures an immutable commit, scans and indexes readable files, and queues source synthesis when a generation model is configured and repository policy allows it. The overview is produced before questions. Questions search both source and OKF knowledge, reopen the underlying code, expand available static relationships within limits, and assess each proposed claim against source. OKF is also downloadable as a portable bundle.

Without a model, capture, source search, Go parsing, optional SCIP import, inventory and extracted declarations work locally. They do **not** count as semantic understanding. README headings, filenames and repeated words no longer generate purpose claims. Model assessments can still be wrong; use the source citations and see the [verification record](docs/rework-verification.md).

## Run with Docker Compose

Requirements: Git to obtain this tool, Docker with Linux containers, and Docker Compose v2. On Windows use Docker Desktop with its WSL2 backend; on macOS use Docker Desktop; on Linux use Docker Engine with Compose. Runtime verification currently covers Windows with Docker Desktop and the Linux amd64 containers. Other hosts are documented installation targets, not claimed as tested releases.

Clone this tool and open a terminal in its root. On a **new installation only**, create the database password file. Do not overwrite an existing password: the persistent database already uses it.

Linux/macOS:

```sh
umask 077
mkdir -p secrets
openssl rand -base64 32 > secrets/postgres_password.txt
docker compose up --build -d
docker compose ps
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force secrets | Out-Null
$password = [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
[System.IO.File]::WriteAllText((Join-Path (Get-Location) 'secrets/postgres_password.txt'), $password)
Remove-Variable password
docker compose up --build -d
docker compose ps
```

Open [OnboardMePlease](http://127.0.0.1:8765). The application is a local, single-user service bound to loopback. It is not ready for exposure as a shared Internet service. Git is included in the image; capture does not depend on Git or credentials on the host. Only public, credential-free GitHub repositories are accepted; repository scripts are never executed.

## Enable API-key-backed analysis and chat

Create `secrets/openai_api_key.txt` in a local editor and place your OpenAI API key in it, without quotes. Do not paste the key in chat, source files, the browser, command arguments or screenshots. This directory is excluded from Git and Docker build context; Compose mounts the file into the backend at runtime. Restrict its filesystem permissions to your user. The API project needs usable billing/quota; a ChatGPT subscription does not configure this API credential.

The cloud profile uses OpenAI Responses for generation and `text-embedding-3-small` for optional embeddings. The generation model must support structured JSON outputs. The tested default is `gpt-5.4`; `GENERATION_MODEL` can override it. Example, after saving the key:

```sh
docker compose -f compose.yaml -f compose.cloud.yaml up --build -d
```

Select **Cloud opt-in for this repository** when capturing a repository. For an existing snapshot, select it under Recent repositories, check its cloud permission box, and choose **Analyze or resume overview**. This saves the repository's opt-in. Merely mounting a key does not authorize sending every stored repository to the cloud. Source excerpts, derived knowledge and questions may be sent for opted-in repositories. Generation sets `store: false`; this is not a claim about the provider's broader retention policy.

A capture queues analysis automatically when its policy permits it. Large repositories require additional **Analyze or resume overview** runs. Successful batches are cached; restarting or retrying does not repeat those batches with the same source, prompt and model. The UI separates indexed-file counts from reviewed-chunk counts, shows partial results and links every claim to source.

Use **Ask** for technical questions and follow-ups. Answers persist per snapshot. **Start a new question** clears conversational references; **Follow up on this answer** selects a previous turn. Prior answers never count as evidence. **Index semantic vectors** embeds at most 250 missing source chunks and 100 current concepts per run; repeat for larger snapshots. Exact/lexical search and knowledge retrieval also work without vectors. The answer pipeline reports unavailable vectors; when embedding is incomplete, it searches the vectors already present alongside exact/lexical candidates.

To use another model, set `GENERATION_MODEL` before running the same Compose command. In PowerShell use `$env:GENERATION_MODEL='your-model-id'`; in a POSIX shell use `export GENERATION_MODEL='your-model-id'`. The API key is separate from the endpoint URL. This implementation supports OpenAI cloud generation and a local OpenAI-compatible endpoint; Anthropic and arbitrary cloud endpoints are not implemented.

## Local models and native development

Strict-local mode remains the default and never falls back to cloud. Native generation settings are `GENERATION_PROVIDER=local`, `GENERATION_MODEL`, and `LOCAL_GENERATION_URL` pointing to the complete `/v1/chat/completions` URL on a loopback IP. The local server must implement the strict `response_format.json_schema` contract and return JSON with a `stop` finish reason. Local provider compatibility has been checked with HTTP fixtures, not every model server.

Optional local embedding settings are `LOCAL_EMBEDDING_URL`, `LOCAL_EMBEDDING_MODEL` and `LOCAL_EMBEDDING_DIMENSIONS` (1-2000). These endpoints must use an explicit loopback IP. Inside Docker, loopback means the app container: a model listening on the host is not automatically reachable or authorized. Use the native service with a local model, or explicitly arrange a shared network namespace. Docker DNS names and arbitrary remote endpoints are intentionally not accepted by the current local-only privacy gateway.

Native development requires Go matching `go.mod`, Node matching the Dockerfile, Git, and PostgreSQL with pgvector installed. Provide `DATABASE_URL_FILE` with a PostgreSQL connection URL, or `DATABASE_URL` plus `DATABASE_PASSWORD_FILE`. Never commit these files. Build the UI before the Go binary:

```sh
npm --prefix web ci
npm --prefix web run build
go build -o build/onboardmeplease ./cmd/onboardmeplease
```

On Windows use `-o build/onboardmeplease.exe`. Configure `APP_DATA_DIR` for writable private storage, then run the binary. The default listener is `127.0.0.1:8765`. `doctor` checks Git, data storage and database extensions. Native cross-compilation is not equivalent to a tested native installation.

## Data, limits and recovery

- GitHub clones are temporary staging inputs; approved snapshot files live under `APP_DATA_DIR/snapshots/<snapshot-id>/files`. The Compose `app_data` volume contains this directory. Existing host clones and Git credentials are not used.
- PostgreSQL stores inventory, citable chunks, parser relationships, River jobs, versioned overviews, cached analysis units, OKF Markdown, embeddings and snapshot-scoped chat. New migrations are applied automatically at startup and preserve existing data.
- Each analysis run processes up to 8 uncached units, each at most 12 chunks / 24,000 source bytes. Oversized chunks are explicit gaps. The landing overview shows the reviewed summary; detailed batch findings remain in searchable OKF. The summary has a separate source budget and cannot imply all reviewed details fit. Up to 600 accepted claims are stored in knowledge; capped results remain partial.
- Question planning adds at most 3 searches to the original question. Ranked source/knowledge results are fused; empty retrieval can relax lexical conjunctions. One adjacent chunk on each side can complete split source without a language parser. These reads and graph expansion share the same source budget. Graph exploration uses visited nodes, 3 hops, 60 shared expansions and bounded fan-out. Dynamic/cross-language links remain unresolved unless supported by source. The diagram shows parser-resolved static relationships, not a runtime trace.
- Source secrets are excluded before indexing, rescanned before model use, and checked in generated output/export. Scanning is conservative and can miss secrets or exclude useful code; inspect the inventory. Keys stay in backend secret files and are absent from job payloads, browser bundles and model inputs.
- HTTP 401 indicates a rejected credential; 429 indicates API quota/rate limiting; 400 indicates a request/model configuration problem. Analysis jobs stop on provider failure and retain completed batches for explicit resume. Errors do not include raw provider bodies. Source search remains available.

`docker compose stop` and `docker compose start` preserve data. With cloud mode, use the same `-f compose.yaml -f compose.cloud.yaml` files when rebuilding/recreating. `docker compose down -v` deletes persistent data and is not a normal upgrade step. Back up the PostgreSQL database and `app_data` volume together while the app is stopped; keep secret files separately protected. See [deployment requirements](docs/readme-contract.md) for the recovery checklist. Restore testing on additional hosts is still required before a broad release.

## Verification and architecture

```sh
go test ./...
go vet ./...
python scripts/check_phase0.py
npm --prefix web run build
```

The Python checks require PyYAML (`python -m pip install PyYAML`). `scripts/test_rework.ps1` runs the database fixture and optionally the explicitly enabled live evaluation in the local Compose installation. Live tests make billable model/embedding requests using the configured backend provider, save only synthetic findings under ignored `work/`, and delete their own temporary database fixture. Unit tests do not contact a model. Existing cases retain their maintainer-review status; passing citation/format checks is not a semantic quality score.

- [Architecture rework and acceptance gates](docs/architecture-rework.md)
- [Current verification, evaluation results and platform matrix](docs/rework-verification.md)
- [Original implementation plan](implementation-plan.md)
- [Canonical prompt library](docs/prompt-library.md) and [packaged registry](prompts/README.md)
- [API contract](api/openapi.yaml)
- [Historical Phase 3 preview](docs/phase-3.md)
