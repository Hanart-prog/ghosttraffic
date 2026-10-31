#!/usr/bin/env python3
"""Verify that demo backends are healthy and started from identical seed state."""

import json
import sys
import urllib.request


def fetch_health(url: str) -> dict:
    with urllib.request.urlopen(f"{url}/health", timeout=5) as response:
        return json.load(response)


def main(urls: list[str]) -> int:
    if len(urls) < 2:
        print("usage: check_stack.py URL URL [URL...]", file=sys.stderr)
        return 2
    digests = set()
    for url in urls:
        try:
            health = fetch_health(url)
        except (OSError, ValueError) as exc:
            print(f"FAIL {url}: {exc}", file=sys.stderr)
            return 1
        print(f"ok   {url} variant={health['variant']} seed_digest={health['seed_digest']}")
        digests.add(health["seed_digest"])
    if len(digests) != 1:
        print("FAIL backends do not share the same seed state", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
