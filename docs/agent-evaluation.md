# Evaluating agent navigation

The checked-in `scripts/benchmark_navigation.py` compares scripted `rg` searches with `omp search` and bounded candidate traversal on the mixed-monolith fixture. Cross-boundary cases use `omp navigate` by default; pass `--workflow search-evidence` to compare the earlier two-command workflow. It measures process wall time, stdout bytes, predeclared source-anchor recall and candidate discovery. It reports the separately timed update. A direct search can use multiple hand-selected terms; these are guided scripts, not independent agents. Returned bytes are not equivalent to model tokens. No timing threshold or agent-speed claim follows from this microbenchmark.

Prepare an isolated copy of `testdata/mixed-monolith` as a Git working tree and build `omp`. With `rg`, Python and the optional grammar environment available, run:

```sh
OMP_SYNTAX_PYTHON=/path/to/venv/bin/python python scripts/benchmark_navigation.py \
  --fixture /path/to/isolated-mixed-monolith --omp /path/to/omp \
  --runs 5 --output benchmark-results.json
```

The command runs `omp update` in the supplied fixture. Use a fresh copy to measure initial indexing; the result states how many files were reused. Repeated measurements alternate order; they do not control operating-system caches or provide statistical confidence on their own. The fixture's README is visible to both approaches and contains an expected trace. The HTTP baseline is given the route to search, and the message baseline is given the topic; this favors a knowledgeable direct searcher and makes the navigation scripts reproducible.

To test the actual product goal, run a separate paired study:

1. Freeze a set of questions, required source anchors, expected uncertainty, and forbidden claims before any runs. The existing `testdata/evals/cases.json` is a synthetic starting point, not a reviewed general benchmark. Include larger pinned repositories and languages actually used by the intended users.
2. Use fresh, independent agent sessions on identical worktree copies. Give both arms the same model, prompt, time/token budget and source access. In the control arm use normal file search/read tools; in the assisted arm also make the built `omp` and its investigation skill available. Do not share generated knowledge or answers between sessions.
3. Counterbalance question order and arm order, repeat with multiple sessions, and record whether the index was cold or warm. Measure initial indexing separately and report both amortized and inclusive time. Capture agent wall time, model/tool calls, token usage, retrieved bytes and failures.
4. Blind the reviewer to the arm. Score source-anchor recall and precision, answer correctness, unsupported claims, correct handling of unresolved links and citations. Record task completion time and failures for every run; do not discard failures from timing results.
5. Publish individual observations and paired differences. Report results by repository, task and language; do not generalize from a single fixture or a few agent runs.

The assisted arm must treat candidate edges as leads and inspect their conditions. A route/topic match alone must not be scored as a proven runtime connection. Ensure both arms respect the same exclusions and data-handling requirements.

### Preliminary agent pilot

On a 1,615-file copy of `golang.org/x/tools` v0.50.0, two fresh sessions answered the same question about the analysis checker's `Action`. The control used direct source search; the assisted session started with `omp investigate` and could also read source. Both returned detailed, cited explanations of action creation, dependency execution and failure handling. A manual review checked their main points; it was not blind.

| Measurement | Direct search | OMP-assisted |
| --- | ---: | ---: |
| Agent wall time | 64.71 s | 60.50 s |
| Commands | 4 | 5, including 2 OMP calls |
| Tool output | 47,874 bytes | 42,069 bytes |

The index was already built, so the times exclude setup. This is one question and one pair, without randomized order or a blind quality score; it does not establish a speedup. The pilot used an investigation build before final output-truncation and note-confidence changes.

`scripts/benchmark_agent_sessions.py` runs this question against separate prepared Git copies, checks their tracked source fingerprints, captures session traces, and records time, command counts, output size, and token usage. The assisted copy needs a current index; the control copy must not contain the OMP index. Run `python scripts/benchmark_agent_sessions.py --help` for paths and options. The harness does not grade answers: review citations, missing facts, and unsupported claims separately. On Windows it enables Codex's unelevated sandbox while ignoring user configuration. Traces can contain source and should be stored accordingly.

### Exploratory flow study

The current CLI was tested with fresh Codex CLI 0.162.1 sessions, the same question and source access in both arms, and a prebuilt index in the assisted arm. Sessions used the same runner settings, but the harness did not record the resolved model. The harness checks tracked-source fingerprints before and after each study, but does not check parity for untracked or ignored files. Tool-output bytes include all completed command output. Input token counts include cached input. These were exploratory questions: scoring was not blinded, and the review criteria were not frozen in advance.

| Repository and question | Direct search | OMP-assisted | Tool output, direct / OMP |
| --- | --- | --- | --- |
| Go tools, 1,615 files: command-line checker through analyzer execution | 84.85 s; 80.26 s | 82.68 s; 83.17 s | 211,319 / 102,785 bytes; 206,114 / 106,771 bytes |
| Kafka, 7,564 files: broker produce request through storage and acknowledgements | 105.50 s | 177.65 s | 169,435 / 186,131 bytes |

Each Go arm ran twice, with arm order reversed; Kafka had one pair, with OMP first. The Go pairs were effectively tied at this sample size, while the OMP run emitted less command output. Both Go answers covered the ordinary checker path, the separate unitchecker path, dependency scheduling and failures. Both Kafka answers covered request dispatch, validation, local append, and acknowledgement handling; the OMP answer traced additional storage and response details. This was a manual review of main points, not a formal correctness score. The Kafka answer difference and single run prevent attributing the time gap to OMP alone.

Both OMP sessions needed source-search fallback. The Go `investigate` results started with unrelated `refactor/rename/mvpkg.go` chunks; the Kafka results started with two unrelated tests and a `DeleteRecords` schema. A subsequent Kafka `search handleProduceRequest` ranked test chunks first, and the agent's evidence traversal followed one of them. Kafka's Scala and Java have text-only indexing here, so graph navigation cannot resolve their cross-file calls.

Separate, single warm replays on the same Kafka index measured 12.80 s for the agent's `investigate` query, 10.88 s for `search handleProduceRequest`, and 10.40 s for the selected `evidence` request. These are standalone replay timings, not per-command timings captured in the agent trace. Each path currently performs a full source-inventory audit; the measurements do not isolate how much time that audit consumes. Index creation and the harness's source-copy verification are excluded from agent times. The Go and Kafka source snapshots contained 1,615 and 7,564 tracked files respectively, and the same OMP binary was used for both.

The first improvements to evaluate are: better ranking for natural-language flow questions and implementation files; a way to reuse freshness verification safely across a short investigation; and clearer language-coverage signals so an agent switches to source search sooner when graph edges cannot resolve a flow. Re-run repeated pairs and score citations and correctness against a frozen rubric before claiming a benefit.

### Ranking, graph expansion, and metadata-audit experiment

The follow-up adds bounded pairwise lexical candidates, source-kind and phrase ranking, and a two-hop graph expansion with prioritized edges. A new opt-in `--fast` mode checks file metadata instead of hashing the entire inventory; returned source excerpts and note dependencies are still checked against content hashes. Full verification remains the default. Metadata mode can miss an uncited change with unchanged size and mtime, so it has a different correctness guarantee.

On the same Kafka index, two warm standalone runs of the benchmarked build produced the following times for the agent's original investigation query, in order full, fast, fast, full:

| Verification | Wall times | First source results |
| --- | --- | --- |
| Full hashes | 18.66 s; 18.13 s | `KafkaApis.scala:481`, `Sender.java:881`, `ReplicaManager.scala:641` |
| Metadata (`--fast`) | 3.80 s; 3.80 s | Same three sources |

These command timings include process startup and query execution, not index creation. Earlier source-level measurements on this machine put one full audit at 21.41 s, an earlier metadata implementation at 7.66 s, and the optimized metadata implementation at 3.66 and 5.73 s in two later runs. After the timed build, a regression fix made metadata mode detect deletion of an already-indexed excluded file; that fix was tested but these timings were not repeated. These small, non-randomized samples establish a local difference, not a platform-wide guarantee.

With `--fast` requested for the assisted agent and normal source verification available to both arms, two Go pairs using a near-final build were **81.18 / 71.77 s** and **74.33 / 70.87 s** (direct / OMP). The assisted agent used two OMP calls per run, and all answers covered the major checker paths and failure handling. These runs preceded a final high-degree edge-prioritization fix and removal of redundant filesystem checks.

Two Kafka pairs on subsequent builds show why more repetitions matter: a near-final build ran **66.33 / 99.04 s** (direct / OMP); the benchmarked build ran **111.23 / 95.94 s**. Each assisted run used two OMP calls with `--fast`. Both arms' answers covered the main request, validation, append and acknowledgement path, though their detail differed. Review was manual and not blinded. The benchmarked build returned the handler and replica-manager files among its first three sources, but its Kafka graph still contained source-order links rather than resolved Java/Scala calls. Indexing was excluded, the resolved model was not recorded, and these sparse timings cannot establish a general speedup.

The fast mode still scans the eligible inventory's metadata on each query. There is no watcher, reliable change journal, automatic refresh, or incremental reverse-dependency graph rebuild yet. Update reuses unchanged chunks but recomputes the graph when the source generation changes. These remaining costs and limits must be addressed and measured before claiming the incremental design is complete.

To reproduce the component timings against an already-indexed disposable repository, set `OMP_BENCH_REPO` to its path and run `go test ./internal/engine -run '^$' -bench '^BenchmarkRepository' -benchtime=1x`. Use the same `OMP_SYNTAX_PYTHON` setting used to create that index. For an agent comparison, add `--omp-fast` to the session harness; its trace and summary show whether the assisted commands used `--fast`.

### VS Code TypeScript study

Repository: [microsoft/vscode](https://github.com/microsoft/vscode), commit `3a25209de7a9acb9c13cb7f114ba76c955c7c67b`; 20,691 tracked files, including 15,064 `.ts` and 257 `.tsx` files. The question and the source-based rubric were frozen in [vscode-command-palette.json](../testdata/evals/vscode-command-palette.json) before the agent runs. Both arms received only the question, not the rubric or anchors. Two fresh-session pairs ran in opposite arm order using Codex CLI 0.162.1; the resolved model was not recorded.

The first index attempt failed after 541.31 seconds on an empty TypeScript file. The retry failed after 105.37 seconds and identified the adapter's missing empty-source field. After that fix, a full index build with warm filesystem caches took 582.26 seconds. It contained 20,685 eligible inventory entries, of which 444 were excluded, and occupied 1,992,830,976 bytes (about 1.86 GiB). The 337,409 relationships comprised 239,059 source-order `next` edges, 98,342 syntax-only candidate calls, six unresolved calls, and two external imports. The TypeScript call candidates are not compiler-resolved edges. The candidate-generation limit was reached.

| Pair | Direct search | OMP with `--fast` | Tool-output bytes: direct / OMP |
| --- | ---: | ---: | ---: |
| 1 | 90.04 s | 116.91 s | 96,916 / 143,268 |
| 2 | 120.42 s | 121.02 s | 105,654 / 139,221 |

All four sessions completed, and both tracked-source fingerprints remained unchanged. Manual, non-blind review gave each answer five fully met rubric items and one partial item: all described the pick sources and extension-registration wait, but omitted the early cancellation check when collecting picks. They covered the principal command service, activation, extension-host bridge, callback, and error paths. This small rubric is not a comprehensive correctness assessment. The OMP-assisted sessions used three OMP calls each and verified claims with ordinary source reads.

In both OMP sessions the first `investigate` result ranked unrelated extension command managers; the agent then searched for a specific identifier and followed a source-order relationship. The subsequent evidence output reported truncated syntax candidates. Separate single warm replays of the three OMP commands took 14.73 s (`investigate`), 13.79 s (`search`), and 12.04 s (`evidence`); those are not timings collected within the agent sessions. `--fast` uses metadata freshness, which is weaker than full hashing. Initial indexing, benchmark copy checks, and model usage costs are excluded from the agent wall times. The index build and size, repeated metadata scans, ranking quality, and candidate-only TypeScript links are material limitations exposed by this study.

For new questions, pass `--question "..."`; use `--pairs 2` to reverse arm order between pairs, or `--first assisted` to start with OMP. Each run needs a new empty output directory. The benchmark question is passed to both agents; expected answers and source anchors should stay outside their prompts.

### Session and ranking follow-up

On the same VS Code snapshot and the same Command Palette question, a scripted three-step sequence (`investigate`, `search`, `evidence`) was run twice in opposite orders on a prebuilt index. Separate `--fast` calls took 44.19 s and 31.96 s; an `omp session --fast`, including its initial audit, took 16.25 s and 15.68 s. Session query time after that audit was about 1.8 s in each run. The two-sample mean fell from 38.08 s to 15.96 s, a 58% reduction for this particular scripted sequence, not a measurement of an agent's end-to-end speed. Index construction was excluded.

These paths have different freshness guarantees: separate calls repeat the metadata inventory check; the session checks it once, verifies returned sources by content, and labels results as a session snapshot until refreshed. It cannot detect every repository change made after the initial check. The new ranking put the relevant Command Palette provider file second among three initial sources for this question, where it had previously been absent; the returned chunk was its configuration, not the execution method. This is one previously examined question, not a held-out ranking evaluation, and the first source was still not the principal execution entry point.

## Larger, multi-step flow checks

`scripts/benchmark_real_flows.py` evaluates a Go analysis-checker flow in `golang.org/x/tools` v0.50.0 and a Scala produce-request flow in Kafka commit `cf4905dc7c9c595c60af9c80f238152e1a4b2ea5`. Prepare isolated Git worktrees with those exact source versions and run `omp update --json` separately before the benchmark. Set `OMP_SYNTAX_PYTHON` consistently if the optional adapter was enabled during indexing.

```sh
python scripts/benchmark_real_flows.py --fixture /path/to/tools-worktree \
  --case go_checker --omp /path/to/omp --runs 3 --output tools-results.json
python scripts/benchmark_real_flows.py --fixture /path/to/kafka-worktree \
  --case kafka_produce --omp /path/to/omp --runs 3 --output kafka-results.json
```

The script verifies the preselected anchor lines before timing. It compares path-scoped and repository-wide `rg` against `omp` search plus bounded traversal; the Kafka case also runs five guided `omp` searches. The direct commands are given the function names from the reviewed flow, and the path-scoped variant is given both relevant files. This is a guided retrieval test, not a cold investigation or an answer-quality study. It reports source-anchor recall, output bytes, states, truncation and whether four chosen Go relationships were compiler-resolved. The first traversal page is measured; a returned cursor means further exploration is not included. Kafka's Scala is text-only, so source-order traversal cannot establish the cross-file call chain.
