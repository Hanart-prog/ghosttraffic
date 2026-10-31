#!/usr/bin/env python3
"""Check a workflow run, using each database's own row counts as evidence."""

import json
import sys
from collections import Counter

MISSING_PRODUCT = "GET /api/products/{product_id}"


def problems_in(run_dir: str, sessions: int) -> list[str]:
    problems = []
    with open(f"{run_dir}/results.jsonl") as file:
        results = [json.loads(line) for line in file]
    for target in ("baseline", "candidate"):
        own = [r for r in results if r["target"] == target]
        failed = Counter(
            (r["endpoint_key"], r["status"], r["error_category"])
            for r in own
            if r["status"] >= 400 or r["error_category"] != "none"
        )
        for key, count in failed.items():
            if not (key[0] == MISSING_PRODUCT and key[1] == 404 and count == 1):
                problems.append(f"{target}: {count} x {key}")
        orders = [r for r in own if r["endpoint_key"] == "POST /api/orders"]
        if sum(r["status"] == 201 for r in orders) != sessions:
            problems.append(f"{target}: expected {sessions} orders")
    return problems


def main(argv: list[str]) -> int:
    if len(argv) != 4:
        print("usage: verify_workflows.py RUN_DIR SESSIONS PLAIN_RUN DB_FACTS", file=sys.stderr)
        return 2
    run_dir, sessions, plain_run, facts = argv[0], int(argv[1]), argv[2], argv[3]
    problems = problems_in(run_dir, sessions)

    # Evidence the test was hard: recorded IDs must fail on the shifted target.
    if not any(p.startswith("candidate") for p in problems_in(plain_run, sessions)):
        problems.append("recorded IDs worked on the shifted target, so this proves nothing")

    baseline, candidate = [line.split() for line in open(facts).read().strip().splitlines()]
    for name, row in (("baseline", baseline), ("candidate", candidate)):
        if int(row[0]) != sessions:
            problems.append(f"{name} database holds {row[0]} orders, expected {sessions}")
    if int(candidate[1]) < 500000 or int(baseline[1]) >= 500000:
        problems.append("targets did not use separate ID ranges")

    for problem in problems:
        print(f"FAIL {problem}", file=sys.stderr)
    if problems:
        return 1
    print(
        f"ok   {sessions} checkouts completed on both targets; databases hold "
        f"{baseline[0]} and {candidate[0]} orders, with carts from {baseline[1]} and {candidate[1]}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
