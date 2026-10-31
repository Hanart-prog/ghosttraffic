#!/usr/bin/env python3
"""Check a replay run against the trace and the backends' own access logs.

The access logs are written by uvicorn, not GhostTraffic, so they are independent
evidence of what each target really received.
"""

import json
import re
import sys
import urllib.parse
from collections import Counter

ACCESS_LINE = re.compile(r'"(\w+) (/api/\S*) HTTP/1\.1" (\d+)')
TARGETS = ("baseline", "candidate")
# Applies at recorded speed only: at higher speeds queueing is a real result.
MAX_P95_DELAY_MS = 100


def load(path: str) -> list[dict]:
    with open(path) as file:
        return [json.loads(line) for line in file]


def normalize(path: str) -> str:
    return urllib.parse.unquote(path)


def access_log(path: str) -> Counter:
    with open(path) as file:
        matches = (ACCESS_LINE.search(line) for line in file)
        return Counter((m[1], normalize(m[2]), int(m[3])) for m in matches if m is not None)


def p95(values: list[float]) -> float:
    ordered = sorted(values)
    return ordered[(len(ordered) * 95 + 99) // 100 - 1]


def check(trace_path: str, run_dir: str, logs: dict[str, str]) -> list[str]:
    trace = {r["request_id"]: r for r in load(trace_path)}
    replayable = {i: r for i, r in trace.items() if r["replayable"]}
    results = load(f"{run_dir}/results.jsonl")
    with open(f"{run_dir}/run.json") as file:
        run = json.load(file)
    speed = run["config"]["speed"]
    problems = []

    if run["status"] != "completed":
        problems.append(f"run status is {run['status']}")
    if not run.get("baseline_seed") or run["baseline_seed"] != run.get("candidate_seed"):
        problems.append("run does not prove equal seed state")

    for target in TARGETS:
        own = [r for r in results if r["target"] == target]
        ids = [r["request_id"] for r in own]
        if len(ids) != len(set(ids)):
            problems.append(f"{target}: duplicate results")
        if set(ids) != set(replayable):
            problems.append(f"{target}: results do not cover exactly the replayable requests")

        sent = [r for r in own if r["error_category"] not in ("dropped", "cancelled")]
        for result in sent:
            due = trace[result["request_id"]]["arrival_offset_ms"] / speed
            if abs(result["scheduled_offset_ms"] - due) > 1e-6:
                problems.append(f"{target}: {result['request_id']} scheduled off its arrival")
            if result["actual_start_offset_ms"] < result["scheduled_offset_ms"]:
                problems.append(f"{target}: {result['request_id']} started early")
        delay = p95([r["scheduling_delay_ms"] for r in sent]) if sent else 0
        if speed == 1 and delay > MAX_P95_DELAY_MS:
            problems.append(f"{target}: p95 scheduling delay {delay:.1f}ms")

        expected = Counter(
            (
                trace[r["request_id"]]["method"],
                normalize(trace[r["request_id"]]["path"]),
                r["status"],
            )
            for r in sent
        )
        received = access_log(logs[target])
        for key in (expected - received).elements():
            problems.append(f"{target}: not in its access log: {key}")
        for key in (received - expected).elements():
            problems.append(f"{target}: in its access log but not in results: {key}")
    return problems


def summarize(trace_path: str, run_dir: str, logs: dict[str, str]) -> str:
    results = load(f"{run_dir}/results.jsonl")
    statuses = {t: Counter(r["status"] for r in results if r["target"] == t) for t in TARGETS}
    per_target = len(results) // 2
    return (
        f"ok   each backend's access log shows the same {per_target} requests as its results; "
        f"statuses {dict(statuses['baseline'])} vs {dict(statuses['candidate'])}"
    )


def main(argv: list[str]) -> int:
    if len(argv) != 4:
        print("usage: verify_run.py TRACE RUN_DIR BASELINE_LOG CANDIDATE_LOG", file=sys.stderr)
        return 2
    logs = dict(zip(TARGETS, argv[2:]))
    problems = check(argv[0], argv[1], logs)
    for problem in problems:
        print(f"FAIL {problem}", file=sys.stderr)
    if problems:
        return 1
    print(summarize(argv[0], argv[1], logs))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
