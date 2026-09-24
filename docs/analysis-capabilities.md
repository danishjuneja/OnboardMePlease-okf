# Analysis capability contract

The application reports capabilities for each artifact and each snapshot. “Language agnostic” means the repository model, search, source citations, and failure behavior are common. It does not mean every language has compiler-accurate call resolution.

| Level | Available output | Requirement | Fallback |
|---|---|---|---|
| Text | Inventory, exact/lexical search, sanitized line citations | Readable source | Mark binary/oversize/inaccessible |
| Syntax | Declarations, structural chunks, lexical references | Tested parser grammar | Text level |
| Semantic | Resolved definitions and references | Supported SCIP or native analyzer | Syntax level |
| Framework | Routes, dependency injection, ORM/event wiring | Tested adapter and configuration | Semantic or syntax level |
| Runtime | Observed execution and environment-specific branch | Explicit trace/instrumentation | Static levels with uncertainty |

Initial syntax packs to test: JavaScript/TypeScript, Go, Python, and Java. The registry can add grammars independently. Initial framework targets are chosen from fixture coverage, not assumed for all projects. Importing a SCIP index is optional; absent SCIP must never prevent text or syntax indexing.

Each capability result records `available`, `unavailable`, or `failed`, extractor version, scope, and reason. Parser errors, dynamic dispatch, missing dependency builds, macros, reflection, and configuration-specific wiring are explicit gaps. A symbol relationship has `resolved`, `heuristic`, or `proposed` status; model output alone can produce only `proposed`.

Component groups represent logical responsibility unless a manifest/entry point supports a runtime-unit claim. UI and deployment descriptions are conditional on actual evidence. Multiple deployment profiles remain distinct. Directory names and same-name symbols do not establish connections.

The coverage view accounts for all inventory members, including disconnected code and excluded material. Percentages, if ever displayed, use a clearly named denominator and never equate file coverage with behavioral completeness.
