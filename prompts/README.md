# Packaged prompt registry

The full baseline wording for the implementation agent (A00) and runtime prompts P00–P11 is in [the prompt library](../docs/prompt-library.md). Exact UTF-8 copies are packaged under `templates/`; `registry.json` records semantic versions, schema references, tool budgets, and SHA-256 checksums. The service validates the embedded registry at startup. The runtime executes P00 with P01 for source units, P04 for overview rollup, P05 for query planning, P06 for independent support assessment, and P07 for answers. OKF Markdown is rendered deterministically from the same accepted claims. The remaining templates are packaged for future targeted use; their presence does not mean they execute.

| ID | Purpose |
|---|---|
| A00 | Implementation-agent handoff; not a runtime prompt |
| P00 | Shared system behavior and untrusted-source boundary |
| P01 | Component discovery |
| P02 | Startup and deployment analysis |
| P03 | Technical flow analysis |
| P04 | Initial overview |
| P05 | Interactive query planning |
| P06 | Evidence and claim assessment |
| P07 | Technical answer |
| P08 | OKF content generation |
| P09 | Incremental knowledge revision |
| P10 | Bounded schema repair |
| P11 | Evaluation assistant; never release authority |

Run `python scripts/package_prompt_library.py` from the project root after changing the baseline wording, then run `go test ./prompts` and `python scripts/check_phase0.py`. The [registry schema](../contracts/prompt-registry.schema.json) and generic [input](../contracts/prompt-input.schema.json) and [output](../contracts/prompt-output.schema.json) envelopes are packaged. The registry points executing tasks to their specific result schemas. Application code validates evidence scope, required arrays, claim limits, support indices and provider policy. P00 documents the runtime projection of the original shared envelope. Prompt and schema checksums participate in analysis cache keys.

Regeneration is explicit; repositories cannot supply prompt overrides. Live quality results and remaining limitations are recorded in [verification](../docs/rework-verification.md).
