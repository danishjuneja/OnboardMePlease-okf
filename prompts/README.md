# Prompt registry baseline

The full baseline wording for the implementation agent (A00) and runtime prompts P00–P11 is in [the prompt library](../docs/prompt-library.md). These are design templates, not wired runtime prompts yet.

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

Phase 1–3 split the baseline into exact UTF-8 template files and fill [the registry schema](../contracts/prompt-registry.schema.json) with schema links, allowed tools, token/call limits, checksums, and fixtures. Missing prompts or checksums are startup errors once a prompt is enabled. The implementation must never silently embed substitute instructions in a handler.

During Phase 0, do not present this directory as an executable prompt registry. The document establishes complete prompt wording and the required contract for implementation.
