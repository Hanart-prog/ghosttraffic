#!/usr/bin/env python3
"""Write explicit workflows for the demo's checkout sessions.

GhostTraffic never infers dependencies. This script knows the shop's routes, and
its output is an ordinary workflow file that a person could have written.
"""

import json
import re
import sys
from collections import defaultdict

ROUTES = [
    ("search", "GET", re.compile(r"^/api/products\?")),
    ("view", "GET", re.compile(r"^/api/products/\d+$")),
    ("create-cart", "POST", re.compile(r"^/api/carts$")),
    ("add-item", "POST", re.compile(r"^/api/carts/(\d+)/items$")),
    ("get-cart", "GET", re.compile(r"^/api/carts/(\d+)$")),
    ("order", "POST", re.compile(r"^/api/orders$")),
]


def classify(record: dict) -> tuple[str, str | None]:
    for name, method, pattern in ROUTES:
        match = pattern.match(record["path"])
        if record["method"] == method and match:
            return name, match.group(1) if match.groups() else None
    raise ValueError(f"not a checkout request: {record['method']} {record['path']}")


def checkout(session: str, records: list[dict]) -> dict | None:
    """Chain one session's six requests, or return None if it is not a checkout."""
    try:
        classified = [(classify(r), r) for r in records]
    except ValueError:
        return None
    if [name for (name, _), _ in classified] != [route[0] for route in ROUTES]:
        return None

    steps = []
    previous = None
    for (name, recorded_id), record in classified:
        step = {"id": name, "request_id": record["request_id"]}
        if previous:
            step["depends_on"] = [previous]
        if name == "create-cart":
            step["extract"] = {"cart_id": "$.cart.id"}
        elif recorded_id is not None:
            literal = f"/api/carts/{recorded_id}"
            step["substitute"] = {"path": {literal: "/api/carts/${cart_id}"}}
        elif name == "order":
            step["substitute"] = {"body": {"$.cart_id": "${cart_id}"}}
        steps.append(step)
        previous = name
    return {"session_id": session, "steps": steps}


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print("usage: make_workflows.py TRACE OUT", file=sys.stderr)
        return 2
    sessions = defaultdict(list)
    with open(argv[0]) as file:
        for line in file:
            record = json.loads(line)
            if record["replayable"]:
                sessions[record["session_id"]].append(record)
    workflows = []
    for session, records in sorted(sessions.items()):
        records.sort(key=lambda r: (r["arrival_offset_ms"], r["request_id"]))
        workflow = checkout(session, records)
        if workflow is not None:
            workflows.append(workflow)
    with open(argv[1], "w") as out:  # JSON is valid YAML
        json.dump({"workflows": workflows}, out, indent=1)
    print(f"ok   wrote {len(workflows)} checkout workflows of {len(sessions)} sessions")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
