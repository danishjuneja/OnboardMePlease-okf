# OnboardMePlease

Understand a large repository through a source-backed overview and technical questions. Capture a public GitHub commit, analyze its code into capability-oriented OKF knowledge, and open citations to inspect the implementation behind an answer.

Capture and lexical source search work without a model. Overview synthesis and answers need a configured generation provider. Analysis is explicit, processes a bounded batch, and caches completed work. Static analysis and model assessments can miss behavior; file coverage is not proof of complete understanding.

## Run locally

Use Docker with Linux containers and Docker Compose v2. From this repository's root, create a database password **only for a new installation**.

PowerShell:

```powershell
New-Item -ItemType Directory -Force secrets | Out-Null
$passwordPath = Join-Path (Get-Location) 'secrets/postgres_password.txt'
if (Test-Path -LiteralPath $passwordPath) { throw 'Database password already exists; keep it.' }
$password = [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))
[System.IO.File]::WriteAllText($passwordPath, $password)
Remove-Variable password
docker compose up --build -d
```

Linux/macOS:

```sh
umask 077
mkdir -p secrets
if ! (set -C; openssl rand -base64 32 > secrets/postgres_password.txt); then
  echo "Password creation failed or the file already exists; keep existing credentials."
  exit 1
fi
docker compose up --build -d
```

For an existing installation, keep its password file and run `docker compose up --build -d` directly. Open [the application](http://127.0.0.1:8765). The default installation makes no model calls.

## Analyze and ask questions

For cloud generation, save your API key in `secrets/openai_api_key.txt` using a local editor, then start the opt-in profile:

```sh
docker compose -f compose.yaml -f compose.cloud.yaml up --build -d
```

Capture a public GitHub URL, select the snapshot, allow cloud analysis for that repository, and choose **Analyze or resume overview**. Repeat to review remaining batches. Use **Ask** for technical questions or follow-ups, and open **Source** citations to inspect the code. Cloud analysis and questions incur API charges; capture does not. See [operation and configuration](docs/operations.md) for local models, model selection, storage and recovery.

The UI provides capture, overview, questions, lexical search and citations. Optional embedding, SCIP, graph, inventory and OKF export APIs are described in [the API contract](contracts/openapi.yaml).

## Repository layout

| Path | Responsibility |
|---|---|
| `cmd/onboardmeplease/` | Executable and startup |
| `internal/` | Backend packages; migrations live with `internal/db` |
| `web/` | React UI; build output is embedded by `internal/webui` |
| `contracts/` | OpenAPI and packaged JSON schemas |
| `prompts/` | Canonical templates and embedded registry |
| `testdata/` | Synthetic repositories and evaluation cases |
| `scripts/` | Named maintenance and verification tools |
| `docs/` | Architecture, operation, development and historical verification |

`data/`, `secrets/`, `work/`, build output and dependencies are local ignored files.

- [Architecture and limits](docs/architecture.md)
- [Development and scripts](docs/development.md)
- [Prompt maintenance](prompts/README.md)
- [Historical verification](docs/verification-history.md)
