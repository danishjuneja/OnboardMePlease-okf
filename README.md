# OnboardMePlease

A local repository evidence CLI for your chosen coding agent. Index a Git working tree, find source, follow supported relationships, and reuse agent-written knowledge with source provenance.

```sh
go build -o omp ./cmd/omp
# From the repository you want to investigate:
omp update --json
omp search "processCancellation" --json
```

The six commands are `update`, `status`, `search`, `evidence`, `knowledge pending`, and `knowledge apply`. There is no service, UI, cloud API integration or model inside the CLI. Your agent performs inference using the [investigation skill](skills/omp-investigate/SKILL.md). Fully offline reasoning requires a local agent; using a cloud agent retains its usual costs.

- [Build, commands and agent setup](docs/operations.md)
- [Architecture and capability limits](docs/architecture.md)
- [Development and reviewed investigation questions](docs/development.md)
- [Knowledge result contract](skills/omp-investigate/references/result-contract.md)

Go has local AST/type-based relationships with explicit gaps. Other languages have text retrieval. Generated knowledge is agent-assessed, not automatically proven. The skill falls back to direct source investigation when retrieval is inadequate.

Source and knowledge state live under `.onboard/` in the target repository. Cache is ignored and rebuildable; managed knowledge can be committed selectively. Embeddings and large-repository hardening are subsequent phases.

The former service is preserved in Git at `c6697e2`. Historical plans under `docs/archive/` and `docs/verification-history.md` describe that service, not the current CLI.
