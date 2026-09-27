# Prompts

`templates/*.txt` is the source of truth for prompt wording. `registry.json` records versions, schema references, budgets and SHA-256 checksums. Go embeds both and validates the registry at startup. There is no separate Markdown copy to keep synchronized.

The current runtime combines P00 with P01 for source units, P04 for overview summaries, P06 for support assessment and P07 for answers. OKF Markdown is rendered deterministically from accepted claims. No prompt has unrestricted tools; the application owns retrieval, source scope, budgets, provider policy and citations.

P02/P03 (targeted startup/flow analysis), P05 (query planning), P08 (OKF generation), P09 (revision), P10 (repair) and P11 (evaluation) are retained templates and are not currently executed. A00 is an implementation handoff, not a runtime prompt.

To change wording, edit the template, update its semantic version in the registry, and run:

```sh
python scripts/package_prompt_library.py
```

Run from the repository root. This refreshes checksums while preserving registry metadata. Optional `go test ./prompts` and `python scripts/check_contracts.py` check packaging integrity. Prompt/schema fingerprints invalidate cached analysis when relevant content changes. Indexed repositories cannot override prompts.
