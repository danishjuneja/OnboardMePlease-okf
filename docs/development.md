# Development

Build with `go build ./...` and run `go test ./...`. The CLI uses Git for repository discovery and embedded SQLite for its index. It does not execute the indexed repository's build or install its dependencies.

For Python and TypeScript syntax support, install `requirements-syntax.txt` in a trusted Python environment and set `OMP_SYNTAX_PYTHON` to its interpreter before indexing and testing. The optional adapter tests run with the same setting. See [operations](operations.md) for setup.

`testdata/mixed-monolith` exercises Go source, cross-language candidates, ambiguous code, and misleading comments. `testdata/evals/cases.json` contains synthetic questions and anchors; it is not a general-purpose benchmark. See [agent evaluation](agent-evaluation.md) for the difference between scripted retrieval timings and end-to-end agent comparisons.

When changing retrieval, preserve source citations, stale-result checks, bounded output, and the distinction between supported relationships and candidates. Knowledge validation checks structure and provenance; it cannot establish whether an agent's interpretation is correct.
