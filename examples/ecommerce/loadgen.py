#!/usr/bin/env python3
"""Send synthetic shop traffic and log exactly what was sent.

Every secret is an obvious fake starting with FAKE-SECRET, so a single search of
a trace proves none survived. The log is the ground truth for verify_trace.py.
"""

import argparse
import json
import random
import threading
import time
import urllib.error
import urllib.request

SECRET = "FAKE-SECRET"
SESSION_HEADER = "X-GhostTraffic-Session"


class Session:
    def __init__(self, base_url: str, name: str, rng: random.Random, sent: list, lock) -> None:
        self.base_url, self.name, self.rng, self.sent, self.lock = base_url, name, rng, sent, lock

    def request(self, method: str, path: str, body=None, raw=None, content_type=None):
        data = raw if raw is not None else None if body is None else json.dumps(body).encode()
        headers = {
            "Accept": "application/json",
            "Authorization": f"Bearer {SECRET}-{self.name}",
            "Cookie": f"sid={SECRET}-{self.name}",
            "X-Forwarded-For": "203.0.113.9",
            SESSION_HEADER: self.name,
        }
        if data is not None:
            headers["Content-Type"] = content_type or "application/json"
        request = urllib.request.Request(
            self.base_url + path, data=data, headers=headers, method=method
        )
        try:
            with urllib.request.urlopen(request, timeout=10) as response:
                status, payload = response.status, response.read()
        except urllib.error.HTTPError as exc:
            status, payload = exc.code, exc.read()
        with self.lock:
            self.sent.append(
                {
                    "session": self.name,
                    "method": method,
                    "path": path,
                    "headers": sorted(headers),
                    "content_type": headers.get("Content-Type"),
                    "body": body,
                    "raw_bytes": None if raw is None else len(raw),
                    "status": status,
                }
            )
        time.sleep(self.rng.uniform(0.002, 0.02))
        return status, json.loads(payload) if payload else None

    def checkout(self) -> None:
        query = self.rng.choice(["shoe", "jacket", "sock", "pack"])
        status, found = self.request(
            "GET", f"/api/products?query={query}&limit=5&access_token={SECRET}-{self.name}"
        )
        assert status == 200 and found["items"], (status, found)
        product = self.rng.choice(found["items"])
        self.request("GET", f"/api/products/{product['id']}")
        customer = self.rng.randint(1, 100)
        status, cart = self.request(
            "POST", "/api/carts", {"customer_id": customer, "password": f"{SECRET}-pw"}
        )
        assert status == 201, (status, cart)
        cart_id = cart["cart"]["id"]
        items = {"product_id": product["id"], "quantity": self.rng.randint(1, 3)}
        status, _ = self.request("POST", f"/api/carts/{cart_id}/items", items)
        assert status == 201, status
        self.request("GET", f"/api/carts/{cart_id}")
        status, _ = self.request(
            "POST",
            "/api/orders",
            {"cart_id": cart_id, "payment": {"card_number": f"{SECRET}-4242", "note": "gift"}},
        )
        assert status == 201, status

    def edge_cases(self) -> None:
        self.request("GET", "/api/products/999999")
        self.request(
            "POST",
            "/api/carts",
            raw=f"customer_id=1&password={SECRET}-form".encode(),
            content_type="application/x-www-form-urlencoded",
        )
        self.request("POST", "/api/carts", raw=b'{"customer_id":')
        oversized = json.dumps({"customer_id": 1, "note": SECRET * 200_000}).encode()
        self.request("POST", "/api/carts", raw=oversized)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True)
    parser.add_argument("--sessions", type=int, default=20)
    parser.add_argument("--seed", type=int, default=1)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    sent: list = []
    lock = threading.Lock()
    failures: list = []

    def run(index: int) -> None:
        name = f"session_{index:02d}"
        session = Session(args.url, name, random.Random(args.seed + index), sent, lock)
        try:
            session.checkout()
        except Exception as exc:  # A thread's exception would otherwise vanish.
            failures.append((name, repr(exc)))

    threads = [threading.Thread(target=run, args=(i,)) for i in range(args.sessions)]
    for thread in threads:
        thread.start()
    for thread in threads:
        thread.join()
    Session(args.url, "edge", random.Random(args.seed), sent, lock).edge_cases()

    with open(args.out, "w") as out:
        for entry in sent:
            out.write(json.dumps(entry) + "\n")
    print(f"sent {len(sent)} requests in {args.sessions} concurrent sessions, plus edge cases")
    if failures:
        raise SystemExit(f"session failures: {failures}")


if __name__ == "__main__":
    main()
