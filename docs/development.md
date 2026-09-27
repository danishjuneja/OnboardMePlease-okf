# Development

Use Go matching `go.mod`, Node matching the Dockerfile, Git, and PostgreSQL with pgvector. The frontend is compiled into `internal/webui/dist` and embedded by Go. Build in this order; parallel frontend and Go builds can race with asset replacement.

```sh
npm --prefix web ci
npm --prefix web run build
go build -o build/onboardmeplease ./cmd/onboardmeplease
```

On Windows use `-o build/onboardmeplease.exe`. Configure database access and `APP_DATA_DIR` using [the configuration reference](operations.md), then run the binary. `doctor`, `migrate` and `serve` are the public commands; `serve` is the default. Native development needs no model for capture and lexical search.

For UI development, `npm --prefix web run dev` starts Vite on loopback and proxies `/v1` to the Go service on port 8765. The production service embeds the built UI and does not run a separate frontend server.

## Maintenance tools

| Script | Purpose |
|---|---|
| `package_prompt_library.py` | Refresh registry checksums from canonical templates |
| `check_contracts.py` | Offline schema/API references, prompt checksums, evaluation anchors and documentation links |
| `materialize_fixtures.py` | Create fresh local Git fixtures for isolated development |
| `smoke_capture.ps1` | Capture a public repository and save `work/capture-smoke.json` |
| `smoke_overview.ps1` | Inspect an existing smoke snapshot's inventory, citations and OKF export |
| `check_persistence.ps1` | Compare saved capture metadata after a database restart |
| `test_integration.ps1` | Run database fixtures in an already-running Compose app; `-Live` explicitly enables billable evaluation |
| `validate_okf.py` | Inspect an exported bundle's structure |

Optional maintainer checks:

```sh
go test ./...
go vet ./...
python -m pip install -r scripts/requirements.txt
python scripts/check_contracts.py
```

Database/live fixtures are opt-in. `test_integration.ps1 -Live` makes paid provider requests, writes synthetic results under ignored `work/`, and uses `testdata/evals/cases.json` as its rubric. Structural checks and source-anchor recall are not answer-accuracy scores. Evaluation cases still require maintainer review.

Edit prompts in [the canonical templates](../prompts/README.md), then explicitly refresh checksums. Keep runtime changes and migration files with their owning packages; avoid new root-level feature folders or parallel contract libraries.

The 2026-09-28 cleanup archived superseded milestone plans and the duplicate prompt library, and removed README preservation checks. The historical verification document describes earlier runs. No test suites or paid evaluations were run for this cleanup.
