# Operation and configuration

Use [the README](../README.md) for first installation. This is a loopback-bound, single-user service. The existing verification record covers Windows with Docker Desktop and Linux amd64 containers; native service installation, other host platforms and backup/restore drills have not been fully verified.

## Models

The cloud Compose overlay configures generation with the default model recorded in `compose.cloud.yaml`. Set `GENERATION_MODEL` to override it before restarting: `$env:GENERATION_MODEL='your-model-id'` in PowerShell or `export GENERATION_MODEL='your-model-id'` in a POSIX shell. Generation needs structured JSON output support. Optional cloud embeddings use `text-embedding-3-small`.

Save the API key in `secrets/openai_api_key.txt`, restrict the file to your user, and keep it outside source control. The backend reads the mounted file. Cloud consent allows approved source excerpts, derived knowledge and questions to reach the provider; `store: false` is a request setting, not a guarantee about broader retention.

Native local generation requires `GENERATION_PROVIDER=local`, `GENERATION_MODEL`, and `LOCAL_GENERATION_URL` set to the full `/v1/chat/completions` URL on a loopback IP. The endpoint must support strict `response_format.json_schema` and a `stop` finish reason. Compatibility has been checked with fixtures, not every model server. Local embeddings need the URL, model and dimensions below. Inside Docker, loopback points to the app container; a host model is not automatically reachable. Use native execution or an explicitly shared network namespace.

## Configuration

Settings are read at startup. Compose supplies database credentials and container binding; `.env.example` documents Compose interpolation and native settings. Native environment variables are read directly by the executable.

| Setting | Meaning / default |
|---|---|
| `APP_LISTEN_ADDR` | Native listener, `127.0.0.1:8765` |
| `APP_CONTAINER_MODE` | `1` permits container binding to `0.0.0.0`; Compose publishes loopback |
| `APP_DATA_DIR` | Snapshot files; native default is the OS user config directory under `OnboardMePlease` |
| `APP_CAPTURE_TIMEOUT` | Capture deadline, `2h`; accepted range `1m`–`24h` |
| `DATABASE_URL_FILE` | Preferred native connection file; takes precedence over `DATABASE_URL` |
| `DATABASE_URL` | PostgreSQL connection URL |
| `DATABASE_PASSWORD_FILE` | Optional password file applied to the URL's user |
| `MODEL_MODE` | `strict_local` by default, or `cloud_opt_in` |
| `GENERATION_PROVIDER` | `local` or `cloud`; absent means generation unavailable |
| `GENERATION_MODEL` | Required model ID when generation is used |
| `LOCAL_GENERATION_URL` | Local structured-generation endpoint |
| `LOCAL_EMBEDDING_URL` | Optional local embeddings endpoint |
| `LOCAL_EMBEDDING_MODEL` | Required with the embedding URL |
| `LOCAL_EMBEDDING_DIMENSIONS` | Required with the embedding URL; `1`–`2000` |
| `OPENAI_API_KEY_FILE` | Backend cloud credential file |

## Storage and recovery

Compose keeps database state in `postgres_data` and snapshot files in `app_data`. Approved files live under `APP_DATA_DIR/snapshots/<snapshot-id>/files`; Git clones are temporary staging inputs. Migrations run at startup and preserve existing data.

Use `docker compose stop` / `docker compose start` to stop and resume. When using cloud mode, use the same `-f compose.yaml -f compose.cloud.yaml` options for recreation or upgrades. `docker compose down -v` deletes persistent data. Do not regenerate the database password during an upgrade.

For backup, stop the app and preserve a PostgreSQL backup and the matching `app_data` snapshot files together, with secret files protected separately. Restore both from the same backup point before starting the matching application version. A complete restore drill remains unverified; do not treat a successful compile as evidence of recoverability.

## Troubleshooting

- Use `docker compose ps`, `docker compose logs app`, and `docker compose exec app /app/onboardmeplease doctor` to check startup, Git and database availability.
- HTTP 401 from generation means a rejected credential; 429 means quota or rate limiting; 400 usually means model/request configuration. Raw provider bodies are not returned to the UI.
- Failed analysis retains completed source units. Fix the provider problem and use **Analyze or resume overview**; analysis failures do not automatically trigger repeated provider charges.
- A partial overview means some source was not reviewed or accepted. Repeated analysis can cover remaining units; excluded or oversized content may remain unresolved.
- With no model, source search still works. With no vectors, lexical source/knowledge retrieval still works. Refine an empty query with a symbol, handler or component name.
