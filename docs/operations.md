# Build and use

Install Go 1.27.1 and Git. From this repository:

```sh
go build -o omp ./cmd/omp
```

On Windows, build with `go build -o omp.exe ./cmd/omp` and use `omp.exe`. Put the binary on PATH, then run in any local Git repository:

```sh
omp update --json
omp status --json
omp status --verbose --json
omp investigate "How does processCancellation work?" --json
omp search "processCancellation" --json
omp navigate "processCancellation" --direction outgoing --kinds calls,references --depth 2 --json
omp evidence <id> --direction incoming --depth 2 --json
omp knowledge pending --limit 20 --json
omp knowledge apply result.json --dry-run --json
omp knowledge apply result.json --json
omp knowledge add --text "A source-backed finding" --evidence <chunk-id> --json
```

`omp --help` lists options. Commands return one version-1 JSON envelope with generation, state, reason codes, warnings, truncation and data. IDs come from search, pending units, relationships and claim provenance. Diagnostics go to stderr. Successful retrieval, including `no_match`, exits zero; operational/argument/validation errors exit one with a reason code. Do not interpret failure as absent implementation.

`matched` means eligible evidence, `partial` means useful evidence with a gap or limit, `no_match` hands off to source investigation, `stale` requires refresh and `unavailable` is an operational failure. Relevance is not answer confidence. See the [result contract](../skills/omp-investigate/references/result-contract.md) for knowledge submission.

`investigate` accepts a question and returns at most three lexical source leads and notes by default, short excerpts, and up to eight relationships plus three related source excerpts from a bounded two-hop traversal. `--limit` can be 1–10. Identifier, phrase, source kind and path matches are ranking heuristics, not semantic understanding; `truncated`, `candidate_budget_reached`, and `result_budget_reached` signal incomplete results. Use source reads and `evidence` for full context. Status returns a capability-gap count by default; `--verbose` restores the full `capability_gaps` list. This changes the default status JSON data shape for consumers that previously read that list.

Retrieval defaults to a full inventory hash audit. `investigate`, `search`, `navigate`, and `evidence` accept `--fast` for an opt-in metadata audit; their JSON envelope reports `verification: metadata` and includes a warning. It checks inventory, size, modification time, mode and configuration, and still content-checks returned sources and note dependencies. A change that preserves metadata elsewhere can be missed, including one that affects graph resolution or negative claims. Use the default full audit where that matters. `--fast` checks every path's metadata; it is not a watcher, and it does not update a stale index automatically.

### Reuse verification in a session

Run `omp session` for a foreground JSON Lines process, or `omp session --fast` to use the weaker metadata audit at startup. It writes one ready envelope, then reads one JSON request per line and writes one envelope per request:

```jsonl
{"command":"investigate","query":"How does the command palette execute a command?"}
{"command":"search","query":"CommandsQuickAccessProvider"}
{"command":"evidence","id":"<source-id>"}
{"command":"refresh"}
{"command":"close"}
```

Requests are sequential. `navigate` and `evidence` accept `direction`, `kinds`, `depth`, `nodes`, and `bytes`; `evidence` also accepts `cursor`. Search and investigation accept `limit`. Each request uses its own database read transaction. A changed index generation invalidates the session; a failed refresh prevents further queries until refresh succeeds. `refresh` rechecks the whole inventory using the session's original verification mode and reloads configuration.

There is no filesystem watcher. Between refreshes, OMP cannot establish whether unrelated files or graph dependencies changed. Every session result is explicitly marked `verification: session_snapshot` and `partial`, with a warning; returned source and note evidence is content-checked. Revalidate before relying on repository-wide conclusions or absence claims. Session mode does not authorize creating knowledge or running repository code.

`knowledge add` stores one focused agent-assessed note using evidence IDs returned by the index. It validates current evidence and exports the same portable JSON/Markdown as structured knowledge. `--kind inference --assumption "..."` records an inference and its assumption; `--confidence low|medium|high` optionally records the author's judgment, not a measured probability. `--dry-run` validates without saving. Notes are not proof, do not mark a unit fully reviewed, and are filtered when their source hashes or configuration change. For negative claims, relationship evidence, conditions, conflicts, or scoped dependencies, use the structured `knowledge apply` contract.

## Optional Python and TypeScript syntax

Create a trusted Python environment outside the target repository and, from this project's checkout, install `requirements-syntax.txt` into it. For example on Windows:

```powershell
python -m venv C:/path/to/omp-syntax-env
C:/path/to/omp-syntax-env/Scripts/python.exe -m pip install -r requirements-syntax.txt
$env:OMP_SYNTAX_PYTHON = 'C:/path/to/omp-syntax-env/Scripts/python.exe'
omp update --json
omp search 'cancelPaidOrder' --json
omp evidence <id> --direction outgoing --kinds calls,http_candidate,message_candidate --depth 2 --json
```

On Linux/macOS, set `OMP_SYNTAX_PYTHON=/path/to/omp-syntax-env/bin/python` in the environment before running `omp`. Set it consistently for every command: changing the interpreter or grammar versions changes the index configuration and requires an update. The CLI itself never installs these packages. With the variable unset, Python and TypeScript use text-only indexing. Only `.py`, `.ts` and `.tsx` have optional grammar mappings; other non-Go languages remain text-only. A syntax match is a navigation candidate, not semantic call resolution. Inspect returned conditions and source. The parser-environment probe is cached and checked against dependency metadata; `omp update --verify` forces a fresh probe and hashes the parser dependencies. Source freshness remains content-audited on read commands.

`navigate` searches and traverses the first ranked source in one command. Its `matches` are compact metadata and `traversal` contains the source and relationships for that seed. Inspect other matches with `evidence`, or use `search` when you need knowledge claim payloads. If traversal returns a cursor, continue it with `omp evidence <seed> --cursor <cursor> --json`.

Cache is rebuildable and ignored by `.onboard/.gitignore`. Commit `.onboard/knowledge` selectively if appropriate for the repository. Keep submission files outside the eligible tree or inside the cache. Never remove a live writer's lock; after a crash, confirm its PID is gone before removing the lock and rerunning update. A failure exporting knowledge after commit is repaired by the next update.

## Agent installation

There is one canonical skill: [omp-investigate](../skills/omp-investigate/SKILL.md). Copy its complete directory into an agent's supported skill directory. For Codex this can be a repository's `.agents/skills/omp-investigate`; for Claude Code it can be `.claude/skills/omp-investigate`. Alternatively point an agent that supports shell tools directly at the canonical SKILL.md. Keep only one maintained source; installed copies must be refreshed when upgrading.

These adapters contain no provider client and require the agent to invoke the installed CLI and parse JSON. Skill discovery and actual model behavior need verification in each agent installation; a compatible file format alone does not establish quality. A local agent is required for fully offline reasoning. The CLI itself runs without a network connection; a cloud agent retains its normal costs and source-handling behavior.
