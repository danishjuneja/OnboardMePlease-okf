"""Compare scripted source navigation with omp on the reviewed mixed fixture.

This is a retrieval microbenchmark, not a measurement of an agent's reasoning.
Run against an isolated Git checkout of testdata/mixed-monolith.
"""
import argparse
import json
import re
import statistics
import subprocess
import time
from pathlib import Path

CASES = [
    {"id": "go_declaration_and_callers", "query": "processCancellation",
     "direct": ["processCancellation"],
     "anchors": [("server/cancel.go", 35), ("server/cancel.go", 43), ("server/cancel.go", 57)]},
    {"id": "typescript_declaration", "query": "cancelPaidOrder",
     "direct": ["cancelPaidOrder"], "anchors": [("web/cancel.ts", 3)]},
    {"id": "python_declaration", "query": "on_message",
     "direct": ["on_message"], "anchors": [("worker/refund_worker.py", 4)]},
    {"id": "http_candidate", "query": "cancelPaidOrder",
     "direct": ["cancelPaidOrder", "POST /api/orders/{id}/cancel"],
     "anchors": [("web/cancel.ts", 3), ("server/main.go", 10)], "graph": "http_candidate"},
    {"id": "message_candidate", "query": "processCancellation",
     "direct": ["refund.requested"],
     "anchors": [("server/cancel.go", 48), ("worker/refund_worker.py", 5)], "graph": "message_candidate"},
]


def run(command, cwd, acceptable=(0,)):
    start = time.perf_counter()
    p = subprocess.run(command, cwd=cwd, capture_output=True)
    elapsed = time.perf_counter() - start
    if p.returncode not in acceptable:
        raise RuntimeError(f"{command[0]} failed ({p.returncode}): {p.stderr.decode(errors='replace')}")
    return p.stdout.decode("utf-8"), elapsed, len(p.stdout)


def direct(case, root):
    hits, total, size = set(), 0.0, 0
    for query in case["direct"]:
        output, seconds, count = run(["rg", "--no-heading", "--line-number", "--fixed-strings", "--", query, "."], root, (0, 1))
        total += seconds
        size += count
        for line in output.splitlines():
            match = re.match(r"^(.*?):(\d+):", line)
            if match:
                hits.add((match.group(1).replace("\\", "/").removeprefix("./"), int(match.group(2))))
    return {"seconds": total, "bytes": size, "anchors": [list(a) for a in case["anchors"] if a in hits]}


def assisted(case, root, binary, workflow):
    if case.get("graph") and workflow == "navigate":
        output, total, size = run([binary, "navigate", case["query"], "--limit", "100", "--direction", "outgoing", "--kinds", case["graph"], "--depth", "2", "--json"], root)
        response = json.loads(output)
        if response["state"] not in ("matched", "partial", "no_match"):
            raise RuntimeError(f"unexpected navigation state: {response['state']}")
        graph = response.get("data", {}).get("traversal", {})
        sources = graph.get("sources", [])
        candidate = any(e["kind"] == case["graph"] and e["resolution"] == "candidate" for e in graph.get("relationships", []))
        found = [list(a) for a in case["anchors"] if any(s["path"] == a[0] and s["start_line"] <= a[1] <= s["end_line"] for s in sources)]
        return {"seconds": total, "bytes": size, "anchors": found, "candidate_seen": candidate}
    output, total, size = run([binary, "search", case["query"], "--limit", "100", "--json"], root)
    response = json.loads(output)
    if response["state"] not in ("matched", "partial", "no_match"):
        raise RuntimeError(f"unexpected search state: {response['state']}")
    hits = response["data"]
    sources = [hit["source"] for hit in hits if hit.get("source")]
    candidate = False
    if case.get("graph"):
        seed = next((h for h in hits if h.get("source", {}).get("symbol") == case["query"]), None)
        if seed:
            output, seconds, count = run([binary, "evidence", seed["id"], "--direction", "outgoing", "--kinds", case["graph"], "--depth", "2", "--json"], root)
            total += seconds
            size += count
            graph = json.loads(output)
            sources.extend(graph.get("data", {}).get("sources", []))
            candidate = any(e["kind"] == case["graph"] and e["resolution"] == "candidate" for e in graph.get("data", {}).get("relationships", []))
    found = [list(a) for a in case["anchors"] if any(s["path"] == a[0] and s["start_line"] <= a[1] <= s["end_line"] for s in sources)]
    return {"seconds": total, "bytes": size, "anchors": found, "candidate_seen": candidate}


def summarize(samples, expected):
    return {"median_seconds": statistics.median(x["seconds"] for x in samples),
            "median_stdout_bytes": statistics.median(x["bytes"] for x in samples),
            "anchors_found_each_run": [len(x["anchors"]) for x in samples],
            "anchors_expected": expected,
            "candidate_seen_each_run": [x.get("candidate_seen", False) for x in samples]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixture", type=Path, required=True)
    parser.add_argument("--omp", type=Path, required=True)
    parser.add_argument("--runs", type=int, default=3)
    parser.add_argument("--workflow", choices=("navigate", "search-evidence"), default="navigate")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    if args.runs < 1 or args.runs > 100:
        parser.error("runs must be 1..100")
    root, binary = args.fixture.resolve(), str(args.omp.resolve())
    raw, index_time, _ = run([binary, "update", "--json"], root)
    update = json.loads(raw)
    if update["state"] != "current":
        raise RuntimeError("index was not current")
    results = []
    for case in CASES:
        samples = {"direct": [], "omp": []}
        for i in range(args.runs):
            for mode in (["direct", "omp"] if i % 2 == 0 else ["omp", "direct"]):
                samples[mode].append(direct(case, root) if mode == "direct" else assisted(case, root, binary, args.workflow))
        results.append({"id": case["id"], "query": case["query"],
                        "direct": summarize(samples["direct"], len(case["anchors"])),
                        "omp": summarize(samples["omp"], len(case["anchors"])), "samples": samples})
    report = {"scope": "scripted retrieval microbenchmark; not an end-to-end agent or answer-quality evaluation",
              "fixture": str(root), "runs_per_case": args.runs, "workflow": args.workflow, "index_seconds": index_time,
              "index_data": update["data"], "index_warnings": update["warnings"], "cases": results}
    encoded = json.dumps(report, indent=2)
    if args.output:
        args.output.write_text(encoded + "\n", encoding="utf-8")
    print(encoded)


if __name__ == "__main__":
    main()
