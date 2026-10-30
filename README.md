# OnboardMePlease (OMP)

OMP helps coding agents find their way around a repository. It builds a local index of source files and relationships, then returns small, source-backed leads for a question. Agents can save findings as notes and check whether the evidence behind them is still current.

## Get started

Install Git and Go 1.27.1, then build the CLI from this repository:

```sh
go build -o omp ./cmd/omp
```

Put the binary on your PATH. In the repository you want to explore, run:

```sh
omp update --json
omp investigate "How does the request handler process an order?" --json
```

On Windows, build with `go build -o omp.exe ./cmd/omp` and use `omp.exe`. The first command builds the index; run it again after the code changes. Investigation returns source paths, line ranges, excerpts, matching notes, and a few related edges. Follow an evidence ID with `omp evidence <id> --json` and read the source before drawing conclusions.

## How it works

OMP inventories eligible files in the Git working tree, splits them into source chunks, and stores searchable text and relationships in a local SQLite index. Go gets compiler-informed relationships; Python and TypeScript can get syntax-aware chunks and candidate links with the optional Tree-sitter setup. Other languages still have text search.

An agent can save a finding with `omp knowledge add --text "..." --evidence <chunk-id> --json`. Notes keep their source fingerprints and can include an author-supplied confidence level. When the evidence changes, OMP marks the finding stale instead of treating it as current. The index lives under `.onboard/cache`; portable notes live under `.onboard/knowledge`.

OMP does not run a model or answer questions itself. A coding agent interprets the leads, checks the source, and explains the result. Candidate relationships are hints, not proof of runtime behavior. Full freshness checks can take time on large repositories.

## Does it help?

The goal is to get an agent to a correct, cited answer with less searching and less repeated context. In one preliminary paired run on a 1,615-file Go repository, the direct-search agent took 64.7 seconds and the OMP-assisted agent took 60.5 seconds; both produced detailed explanations. Index creation was excluded. One question is not enough to establish a general speedup, so we keep the [benchmark method and its limitations](docs/agent-evaluation.md) visible.

## More information

- [Commands, optional language support, and agent setup](docs/operations.md)
- [Architecture and limitations](docs/architecture.md)
- [Development and tests](docs/development.md)
- [Agent investigation skill](skills/omp-investigate/SKILL.md)
