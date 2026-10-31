#!/usr/bin/env python3
"""Compare a trace with the loadgen log. Shares no code with the Go recorder."""

import hashlib
import json
import sys
from collections import Counter

REDACTED = "[REDACTED]"
SENSITIVE_KEYS = {"password", "card_number"}
SENSITIVE_PARAMS = {"access_token"}
MAX_BODY_BYTES = 1 << 20
ALLOWED_HEADERS = {"accept", "content-type"}


def load(path: str) -> list[dict]:
    with open(path) as file:
        return [json.loads(line) for line in file]


def sanitize(value):
    if isinstance(value, dict):
        return {k: REDACTED if k in SENSITIVE_KEYS else sanitize(v) for k, v in value.items()}
    if isinstance(value, list):
        return [sanitize(v) for v in value]
    return value


def sanitize_path(path: str) -> str:
    base, _, query = path.partition("?")
    if not query:
        return path
    params = []
    for param in query.split("&"):
        key = param.split("=", 1)[0]
        params.append(f"{key}={REDACTED}" if key in SENSITIVE_PARAMS else param)
    return f"{base}?{'&'.join(params)}"


def session_id(name: str) -> str:
    return "s_" + hashlib.sha256(name.encode()).hexdigest()[:12]


def expected_record(sent: dict) -> tuple:
    """What the trace should hold for one sent request, as a comparable key."""
    raw = sent["raw_bytes"]
    body = None
    if raw is None:
        replayable, truncated = True, False
        body = sanitize(sent["body"])
    else:
        replayable, truncated = False, raw > MAX_BODY_BYTES
    return (
        session_id(sent["session"]),
        sent["method"],
        sanitize_path(sent["path"]),
        json.dumps(body, sort_keys=True),
        sent["status"],
        replayable,
        truncated,
    )


def actual_record(record: dict) -> tuple:
    return (
        record["session_id"],
        record["method"],
        record["path"],
        json.dumps(record["body_json"], sort_keys=True),
        record["capture"]["status"],
        record["replayable"],
        record.get("body_truncated", False),
    )


def check(trace_path: str, sent_path: str) -> list[str]:
    with open(trace_path, "rb") as file:
        raw_trace = file.read()
    records, sent = load(trace_path), load(sent_path)
    problems = []

    for needle in [b"FAKE-SECRET", b"203.0.113.9", b"session_0", b"Bearer"]:
        if needle in raw_trace:
            problems.append(f"trace contains forbidden bytes {needle!r}")

    expected, actual = Counter(map(expected_record, sent)), Counter(map(actual_record, records))
    if len(records) != len(sent):
        problems.append(f"{len(sent)} sent but {len(records)} recorded")
    for key in (expected - actual).elements():
        problems.append(f"missing from trace: {key}")
    for key in (actual - expected).elements():
        problems.append(f"unexpected in trace: {key}")

    for record in records:
        extra = {name.lower() for name in record["headers"]} - ALLOWED_HEADERS
        if extra:
            problems.append(f"{record['request_id']} stored headers {sorted(extra)}")

    arrival = sorted(records, key=lambda r: (r["arrival_offset_ms"], r["request_id"]))
    if arrival and arrival[0]["arrival_offset_ms"] != 0:
        problems.append("first arrival is not at offset 0")
    for name in {s["session"] for s in sent}:
        sent_order = [(s["method"], sanitize_path(s["path"])) for s in sent if s["session"] == name]
        hashed = session_id(name)
        trace_order = [(r["method"], r["path"]) for r in arrival if r["session_id"] == hashed]
        if sent_order != trace_order:
            problems.append(f"{name}: request order differs from what was sent")
    return problems


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: verify_trace.py TRACE SENT_LOG", file=sys.stderr)
        return 2
    problems = check(argv[0], argv[1])
    for problem in problems:
        print(f"FAIL {problem}", file=sys.stderr)
    if problems:
        return 1
    records = load(argv[0])
    unreplayable = sum(not r["replayable"] for r in records)
    print(
        f"ok   {len(records)} requests match what was sent, in per-session order; "
        f"{unreplayable} marked unreplayable; no fake secret in the trace"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
