#!/usr/bin/env python3
"""Fail unless a report reaches the verdicts this scenario must produce."""

import argparse
import json
import sys

UNEXPLAINED = ("status_mismatch", "body_mismatch", "transport_failure", "not_comparable")

# Workflows keep each session in order, so identical backends should agree
# exactly. Judged on the median trial, like verdicts: one stalled trial is
# reported, while a real behavior change affects every trial.
MAX_DIFFERENCE_RATE = 0


def check(report: dict, expect: str, endpoint: str | None) -> list[str]:
    problems = []
    verdicts = {e["key"]: e["verdict"] for e in report["endpoints"]}
    if report["provenance"]["status"] != "completed":
        problems.append(f"run status is {report['provenance']['status']}")

    measured = {k: v for k, v in verdicts.items() if v != "insufficient_samples"}
    if not measured:
        problems.append("no endpoint had enough samples to judge")
    rates = sorted(t["different"] / t["compared"] for t in report["trial_differences"])
    if rates and rates[len(rates) // 2] > MAX_DIFFERENCE_RATE:
        for c in report["comparisons"]:
            if c["classification"] in UNEXPLAINED:
                problems.append(f"{c['endpoint']}: {c['count']} {c['classification']}")

    if expect == "clean":
        for key, verdict in measured.items():
            if verdict != "no_regression":
                problems.append(f"false alarm: {key} is {verdict}")
    else:
        if verdicts.get(endpoint) != "confirmed_regression":
            problems.append(f"missed: {endpoint} is {verdicts.get(endpoint)}")
        for key, verdict in measured.items():
            if key != endpoint and verdict == "confirmed_regression":
                problems.append(f"wrong endpoint blamed: {key} is {verdict}")
    return problems


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("report")
    parser.add_argument("--expect", choices=["clean", "regression"], required=True)
    parser.add_argument("--endpoint")
    args = parser.parse_args(argv)
    if args.expect == "regression" and not args.endpoint:
        parser.error("--expect regression needs --endpoint")
    with open(args.report) as file:
        report = json.load(file)
    problems = check(report, args.expect, args.endpoint)
    for problem in problems:
        print(f"FAIL {problem}", file=sys.stderr)
    if problems:
        return 1
    print(f"ok   report verdict {report['verdict']} is what this scenario requires")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
