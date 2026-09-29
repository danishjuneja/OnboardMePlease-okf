# Build and use

Install Go 1.27.1 and Git. From this repository:

```sh
go build -o omp ./cmd/omp
```

Use `omp.exe` on Windows. Put the binary on PATH, then run in any local Git repository:

```sh
omp update --json
omp status --json
omp search "processCancellation" --json
omp evidence <id> --direction incoming --depth 2 --json
omp knowledge pending --limit 20 --json
omp knowledge apply result.json --dry-run --json
omp knowledge apply result.json --json
```

`omp --help` lists options. Commands return one version-1 JSON envelope with generation, state, reason codes, warnings, truncation and data. IDs come from search, pending units, relationships and claim provenance. Diagnostics go to stderr. Successful retrieval, including `no_match`, exits zero; operational/argument/validation errors exit one with a reason code. Do not interpret failure as absent implementation.

`matched` means eligible evidence, `partial` means useful evidence with a gap or limit, `no_match` hands off to source investigation, `stale` requires refresh and `unavailable` is an operational failure. Relevance is not answer confidence. See the [result contract](../skills/omp-investigate/references/result-contract.md) for knowledge submission.

Cache is rebuildable and ignored by `.onboard/.gitignore`. Commit `.onboard/knowledge` selectively if appropriate for the repository. Keep submission files outside the eligible tree or inside the cache. Never remove a live writer's lock; after a crash, confirm its PID is gone before removing the lock and rerunning update. A failure exporting knowledge after commit is repaired by the next update.

## Agent installation

There is one canonical skill: [omp-investigate](../skills/omp-investigate/SKILL.md). Copy its complete directory into an agent's supported skill directory. For Codex this can be a repository's `.agents/skills/omp-investigate`; for Claude Code it can be `.claude/skills/omp-investigate`. Alternatively point an agent that supports shell tools directly at the canonical SKILL.md. Keep only one maintained source; installed copies must be refreshed when upgrading.

These adapters contain no provider client and require the agent to invoke the installed CLI and parse JSON. Skill discovery and actual model behavior need verification in each agent installation; a compatible file format alone does not establish quality. A local agent is required for fully offline reasoning. The CLI itself runs without a network connection; a cloud agent retains its normal costs and source-handling behavior.
