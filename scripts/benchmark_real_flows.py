"""Guided, repeatable source-anchor retrieval on pinned local repository copies.

This is not a blind-agent or answer-quality benchmark. Paths and direct search
patterns are hand-selected after source review and are intentionally given to
both workflows. Indexing is measured separately by the caller.
"""
import argparse
import json
import re
import statistics
import subprocess
import time
from pathlib import Path

CASES = {
    "go_checker": {
        "query": "Run go analysis internal checker",
        "seed": ("go/analysis/internal/checker/checker.go", 82),
        "kinds": "calls,references,next",
        "depth": 8,
        "anchors": [
            ("go/analysis/internal/checker/checker.go", 82, "func Run("),
            ("go/analysis/internal/checker/checker.go", 185, "checker.Analyze("),
            ("go/analysis/checker/checker.go", 147, "func Analyze("),
            ("go/analysis/checker/checker.go", 209, "execAll(roots)"),
            ("go/analysis/checker/checker.go", 241, "func execAll("),
            ("go/analysis/checker/checker.go", 246, "act.exec()"),
            ("go/analysis/checker/checker.go", 258, "func (act *Action) exec()"),
            ("go/analysis/checker/checker.go", 260, "func (act *Action) execOnce()"),
        ],
        "edges": [
            ("go/analysis/internal/checker/checker.go", 185, "calls", "Analyze"),
            ("go/analysis/checker/checker.go", 209, "calls", "execAll"),
            ("go/analysis/checker/checker.go", 246, "calls", "exec"),
            ("go/analysis/checker/checker.go", 258, "references", "execOnce"),
        ],
        "direct": [
            ("go/analysis/internal/checker/checker.go", ["func Run(", "checker.Analyze("]),
            ("go/analysis/checker/checker.go", ["func Analyze(", "execAll(roots)", "func execAll(", "act.exec()", "func (act *Action) exec()", "func (act *Action) execOnce()"]),
        ],
    },
    "kafka_produce": {
        "query": "handleProduceRequest KafkaApis",
        "seed": ("core/src/main/scala/kafka/server/KafkaApis.scala", 175),
        "kinds": "next",
        "depth": 8,
        "anchors": [
            ("core/src/main/scala/kafka/server/KafkaApis.scala", 175, "handleProduceRequest"),
            ("core/src/main/scala/kafka/server/KafkaApis.scala", 401, "def handleProduceRequest"),
            ("core/src/main/scala/kafka/server/KafkaApis.scala", 548, "replicaManager.handleProduceAppend"),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 697, "def handleProduceAppend"),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 767, "appendRecords("),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 638, "def appendRecords("),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 653, "appendRecordsToLeader("),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 586, "def appendRecordsToLeader("),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 597, "appendToLocalLog("),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 1376, "def appendToLocalLog("),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", 1415, "partition.appendRecordsToLeader("),
        ],
        "edges": [],
        "direct": [
            ("core/src/main/scala/kafka/server/KafkaApis.scala", ["handleProduceRequest", "replicaManager.handleProduceAppend"]),
            ("core/src/main/scala/kafka/server/ReplicaManager.scala", ["handleProduceAppend(", "appendRecords(", "appendRecordsToLeader(", "appendToLocalLog("]),
        ],
        "guided_queries": ["handleProduceRequest KafkaApis", "handleProduceAppend ReplicaManager", "appendRecords ReplicaManager", "appendRecordsToLeader ReplicaManager", "appendToLocalLog ReplicaManager"],
    },
}


def run(args, root):
    start = time.perf_counter()
    p = subprocess.run(args, cwd=root, capture_output=True, timeout=120)
    sec = time.perf_counter() - start
    if p.returncode not in (0, 1) or (p.returncode == 1 and args[0] != "rg"):
        raise RuntimeError(f"{args[0]} exited {p.returncode}: {p.stderr.decode(errors='replace')}")
    return p.stdout.decode("utf-8"), sec, len(p.stdout)


def check_oracle(root, case):
    for path, line, needle in case["anchors"]:
        content = (root / path).read_text(encoding="utf-8").splitlines()
        if needle not in content[line-1]:
            raise ValueError(f"oracle does not match {path}:{line}: {needle}")


def direct(root, case, repo_wide=False):
    seconds = size = 0
    lines = {}
    searches = case["direct"]
    if repo_wide:
        patterns = list(dict.fromkeys(pattern for _, items in searches for pattern in items))
        searches = [(".", patterns)]
    for path, patterns in searches:
        cmd = ["rg", "--no-heading", "--with-filename", "--line-number", "--fixed-strings", "--context", "3"]
        if repo_wide:
            cmd += ["--glob", "*.go" if case["kinds"] != "next" else "*.scala"]
        for p in patterns:
            cmd += ["-e", p]
        output, elapsed, count = run(cmd + ["--", path], root)
        seconds += elapsed
        size += count
        for row in output.splitlines():
            match = re.match(r"^(.*?)(?::|-)(\d+)(?::|-)(.*)$", row)
            if match:
                lines[(match.group(1).replace("\\", "/").removeprefix("./"), int(match.group(2)))] = match.group(3)
    found = [f"{p}:{line}" for p, line, token in case["anchors"] if token in lines.get((p, line), "")]
    return {"seconds":seconds, "bytes":size, "commands":len(searches), "anchors":found}


def extract_sources(response):
    return [hit["source"] for hit in response.get("data", []) if hit.get("source")]


def score_sources(sources, case):
    found = []
    for path, line, token in case["anchors"]:
        for s in sources:
            if s["path"] == path and s["start_line"] <= line <= s["end_line"]:
                pieces = s["text"].splitlines()
                offset = line - s["start_line"]
                if offset < len(pieces) and token in pieces[offset]:
                    found.append(f"{path}:{line}")
                    break
    return found


def guided_search(root, case, binary):
    sources, timings, size, states = [], 0, 0, []
    for query in case["guided_queries"]:
        output, elapsed, count = run([binary, "search", query, "--limit", "100", "--json"], root)
        response = json.loads(output)
        states.append(response["state"])
        sources += extract_sources(response)
        timings += elapsed
        size += count
    return {"seconds":timings, "bytes":size, "commands":len(case["guided_queries"]),
            "anchors":score_sources(sources, case), "states":states}


def graph(root, case, binary):
    output, seconds, size = run([binary, "search", case["query"], "--limit", "100", "--json"], root)
    response = json.loads(output)
    sources = extract_sources(response)
    seed_path, seed_line = case["seed"]
    seed = next((s for s in sources if s["path"] == seed_path and s["start_line"] <= seed_line <= s["end_line"]), None)
    edges, state, reasons, truncated, cursor, graph_sources = [], "seed_not_found", [], False, False, []
    if seed:
        output, elapsed, count = run([binary, "evidence", seed["id"], "--direction", "outgoing", "--kinds", case["kinds"], "--depth", str(case["depth"]), "--nodes", "200", "--bytes", "1048576", "--json"], root)
        seconds += elapsed
        size += count
        walk = json.loads(output)
        state, reasons, truncated = walk["state"], walk["reason_codes"], walk["truncated"]
        if isinstance(walk.get("data"), dict):
            graph_sources = walk["data"].get("sources", [])
            sources += graph_sources
            edges = walk["data"].get("relationships", [])
            cursor = bool(walk["data"].get("cursor"))
    found_edges = [f"{p}:{line} {kind} {name}" for p, line, kind, name in case["edges"]
                   if any(e["path"] == p and e["line"] == line and e["kind"] == kind and e["resolution"] == "compiler_resolved" and name in e["target"] for e in edges)]
    return {"seconds":seconds, "bytes":size, "commands":1 + int(seed is not None),
            "search_state":response["state"], "search_truncated":response["truncated"], "seed_found":seed is not None,
            "state":state, "reasons":reasons, "truncated":truncated, "cursor":cursor,
            "anchors":score_sources(sources, case), "traversal_anchors":score_sources(graph_sources, case),
            "graph_sources":len(graph_sources), "graph_edges":len(edges), "supported_edges":found_edges}


def stats(samples, required):
    return {"median_seconds":statistics.median(s["seconds"] for s in samples),
            "median_bytes":statistics.median(s["bytes"] for s in samples),
            "anchors_expected":required, "anchors_each_run":[len(s["anchors"]) for s in samples],
            "samples":samples}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--fixture", type=Path, required=True)
    ap.add_argument("--case", choices=CASES, required=True)
    ap.add_argument("--omp", type=Path, required=True)
    ap.add_argument("--runs", type=int, default=3)
    ap.add_argument("--output", type=Path, required=True)
    args = ap.parse_args()
    if args.runs < 1 or args.runs > 100:
        ap.error("runs must be 1..100")
    root, binary, case = args.fixture.resolve(), str(args.omp.resolve()), CASES[args.case]
    check_oracle(root, case)
    samples = {"direct": [], "direct_repo": [], "graph": []}
    if case.get("guided_queries"):
        samples["guided_omp"] = []
    for i in range(args.runs):
        modes = list(samples)
        if i % 2: modes.reverse()
        for mode in modes:
            f = {"direct": lambda: direct(root, case), "direct_repo": lambda: direct(root, case, True), "graph": lambda: graph(root, case, binary),
                 "guided_omp": lambda: guided_search(root, case, binary)}[mode]
            samples[mode].append(f())
    report = {"scope":"guided source-anchor retrieval; no independent agents or answer review",
              "fixture":str(root), "case":args.case, "runs":args.runs,
              "anchors":case["anchors"], "expected_compiler_edges":case["edges"],
              "results":{mode:stats(values, len(case["anchors"])) for mode, values in samples.items()}}
    args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    for mode, result in report["results"].items():
        print(f"{args.case} {mode}: {result['median_seconds']:.3f}s, anchors {result['anchors_each_run']}/{result['anchors_expected']}, median bytes {result['median_bytes']}")
        if mode == "graph":
            for sample in result["samples"]:
                print(f"  seed={sample['seed_found']}, state={sample['state']}, graph_sources={sample['graph_sources']}, edges={sample['graph_edges']}, compiler_edges={len(sample['supported_edges'])}/{len(case['edges'])}, cursor={sample['cursor']}, reasons={sample['reasons']}")


if __name__ == "__main__":
    main()
