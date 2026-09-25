# Architecture rework verification

Date: 2026-09-25. This is an implementation/evaluation record, not a claim of universal repository understanding.

## Delivered path

- Removed README/manifest-heading and filename/token-frequency purpose interpretation. Without generation, the UI explicitly shows extracted declarations and zero semantic coverage.
- Source units cover indexed content independently of questions. Each bounded run processes uncached units; successful work is persisted by snapshot/source, model and prompt/schema fingerprints. Coverage accounts for unexamined and oversized chunks. Source content is loaded by unit rather than loading the whole monolith into model memory.
- Packaged P00/P01/P04/P05/P06/P07 execute in the generation path. Source claims and gap statements are assessed against the supplied source; IDs, limits and scope are checked deterministically. The same accepted claims produce the overview and actual stored OKF Markdown, including source footnotes and inference limits.
- Questions fuse exact/lexical and optional vector candidates across source and knowledge. Knowledge retrieval reopens its source dependencies. Graph traversal and source reads share fixed budgets. Previous questions resolve conversational references; prior answers do not count as evidence. Answers and citations persist within the snapshot.
- Optional embedding jobs include both source and current knowledge. Old knowledge versions are excluded after index changes. Local endpoints cannot fall back to cloud; cloud requires installation and repository opt-in. Provider credentials are read only by the backend.

## Automated checks

Go cross-compilation succeeded for Windows amd64, Linux arm64 and macOS arm64; these are compile checks only.

The Windows Go suite and `go vet ./...` pass, including secret-input blocking, refused/truncated model output, redirects, wrong citation IDs, duplicate support indices, full partition accounting, and existing graph cycle tests. `scripts/check_phase0.py` validates schemas, prompt checksums, original README preservation, evaluation anchors and documentation links. The Linux amd64 Docker image builds its React production bundle and Go service.

The PostgreSQL integration fixture passed. It removes the README, uses the real text/Go indexing pipeline, exercises synthesis with a deterministic provider, checks cached units, verifies actual OKF-to-source retrieval, follows a conversation, rejects a foreign parent turn, and invalidates knowledge after a changed source hash. This verifies mechanics, not model intelligence.

## Live evaluation

The first API attempt failed with quota exhaustion; no fabricated fallback overview was published. After quota became available, live evaluation used `gpt-5-mini` and `text-embedding-3-small` on the existing mixed-monolith fixture with its README removed.

An initial run produced no accepted purpose. A later run exposed overcautious Go route warnings and some overstatements in gap text. These failures motivated explicit conditional-source semantics, separate local-unit versus repository-purpose instructions, and source assessment of gap statements. They were not treated as passing quality checks.

Pre-fix retrieval comparison on the paid-cancellation question found 5/7 required source anchors with source lexical retrieval, 5/7 with graph expansion, 6/7 with OKF, and 7/7 with all enabled. Graph did not improve this fixture; no new graph database or parser pack was justified. These are source-anchor recall measurements, not answer-accuracy percentages. The final full-model run used `gpt-5.4`: source lexical and source+graph found 5/7 cancellation anchors; source+OKF, source+vectors and the combined pipeline each found 7/7. Missing-consumer and guarded-cycle anchor recall were 1/1 and 2/2 across variants. On this small fixture, vectors and OKF independently recovered the missing anchors; neither improved recall further when combined. No broader performance conclusion is justified.

Manual inspection of that run found the required client-to-handler-to-worker path, the 202-after-success condition, the no-op persistence stub, an explicitly unresolved broker connection, scoped missing-consumer statements, and the bounded replay recursion. The full model produced purpose without a README. The mini models completed some runs but also accepted misleading persistence wording, so the cloud default is `gpt-5.4`, with model selection configurable. Provider success and schema validation alone did not count as a quality pass.

The exact synthetic artifacts are in ignored `work/live-evaluation-54full.json` and `work/live-evaluation-54full.overview.json`. They are local review artifacts, not a maintainer-approved benchmark score.

The real reference repository, `mahimathacker/repochat-ai`, was captured at commit `27fb3fbf25c5b01e73713b6c10e46583b2638c99` (31 artifacts, 26 indexed, three excluded, two unsupported). Its first completed run exposed a separate integration bug: the overview generator used application coverage counts, but the support checker received only source, so it rejected the otherwise supported purpose paragraph. Reassessing the exact same draft with the application coverage ledger accepted that paragraph. A regression check now preserves this context. The landing overview now shows the assessed summary instead of duplicating every source-batch finding; detailed findings remain in OKF retrieval.

The resumed run reviewed all 132 indexed chunks in 16 batches and published a 14-claim overview with a source-derived purpose. It correctly remains partial because excluded/unsupported artifacts are outside that review. The capability OKF ZIP passed the v0.2 structural validator. The run produced many fine-grained concepts, including lockfile findings; concept granularity remains a quality limitation rather than evidence of richer understanding.

A broad UI question without vectors initially returned no candidates because conjunctive lexical matching required every planned term. A bounded disjunctive fallback now runs only when the initial retrieval finds nothing, searches both source and OKF, and retains the existing context budgets and support checks. The PostgreSQL regression includes an unmatched query term to exercise this failure directly.

The next UI answer retrieved source and OKF and correctly traced the backend, but omitted the second chunk of `rag.py`, leaving response construction unresolved. Retrieval now reads one immediately adjacent chunk on each side of selected source within the same budget. The database regression verifies that it retrieves the continuation and does not recursively walk the file. Earlier synthetic ablation numbers above predate this adjacency change; they are not presented as new measurements of the final retrieval variant.

The final live retry retrieved 10 source chunks and five OKF concepts without requiring vectors. Its accepted answer covered browser state and HTTP requests, API validation/failure paths, splitting, OpenAI embeddings, persistent Chroma storage, repository-scoped retrieval, and Anthropic generation. Both `rag.py` chunks and the frontend chunks were available. The answer rendered in the browser, and its `rag.py:81–94` citation opened the expected source. No static graph links were reported for this Python/TypeScript snapshot, consistent with the available parser coverage. Provider/model output varies; this successful example is not a guarantee for every question.

Local artifacts for this check are `work/rework-reference-overview.json`, `work/rework-reference-answer-final.json`, and `work/rework-reference.zip`. The final overview still flags the excluded repository-loader implementation instead of inventing its GitHub fetch behavior. All source-code changes passed the Go suite, vet, frontend build, database regressions, prompt/schema checks, and whitespace check before handoff.

The evaluation harness reads the existing cases and writes synthetic overview, answers and retrieval comparisons under ignored `work/`. It does not change `pending_maintainer_review` into an approval. Maintainer review and larger pinned repositories remain necessary before describing this as a production-quality general monolith explainer.

## Platform matrix

| Target | Evidence | Status |
|---|---|---|
| Windows x64 with Docker Desktop / Linux amd64 containers | Compose build, migrations, HTTP capture, real model requests and PostgreSQL integration | Tested in this workspace |
| Linux amd64 container runtime | Service and integration binary execute in the image | Tested |
| Native Windows | Go tests/vet and build tooling | Full native service/model/database setup not tested |
| Linux host with Compose | Same image and documented commands | Host installation not independently tested |
| macOS with Compose | Documented commands | Not tested |
| Native Linux/macOS and arm64 | Cross-compiled Windows amd64, Linux arm64 and macOS arm64 binaries | Runtime not tested |

## Remaining limits

Model support checks are fallible. Semantic depth varies by parser; text retrieval remains available for other languages. Bounded rollups and questions can miss evidence. Large repositories need resume runs, and the original SCIP embedded-source requirement remains. Local model compatibility, multi-user deployment, private repositories, independent host installs and full backup/restore drills are outside the tested release surface. These are explicit limits rather than hidden claims that the architecture is complete for every repository.
