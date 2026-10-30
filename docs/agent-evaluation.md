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

## Larger, multi-step flow checks

`scripts/benchmark_real_flows.py` evaluates a Go analysis-checker flow in `golang.org/x/tools` v0.50.0 and a Scala produce-request flow in Kafka commit `cf4905dc7c9c595c60af9c80f238152e1a4b2ea5`. Prepare isolated Git worktrees with those exact source versions and run `omp update --json` separately before the benchmark. Set `OMP_SYNTAX_PYTHON` consistently if the optional adapter was enabled during indexing.

```sh
python scripts/benchmark_real_flows.py --fixture /path/to/tools-worktree \
  --case go_checker --omp /path/to/omp --runs 3 --output tools-results.json
python scripts/benchmark_real_flows.py --fixture /path/to/kafka-worktree \
  --case kafka_produce --omp /path/to/omp --runs 3 --output kafka-results.json
```

The script verifies the preselected anchor lines before timing. It compares path-scoped and repository-wide `rg` against `omp` search plus bounded traversal; the Kafka case also runs five guided `omp` searches. The direct commands are given the function names from the reviewed flow, and the path-scoped variant is given both relevant files. This is a guided retrieval test, not a cold investigation or an answer-quality study. It reports source-anchor recall, output bytes, states, truncation and whether four chosen Go relationships were compiler-resolved. The first traversal page is measured; a returned cursor means further exploration is not included. Kafka's Scala is text-only, so source-order traversal cannot establish the cross-file call chain.
