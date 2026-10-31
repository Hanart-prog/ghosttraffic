# Limitations

## Current state

Capture, replay, and reports work. `serve` validates its flags and then exits 1. See the status table in [architecture.md](architecture.md).

## By design

- Replay is not fully deterministic. Thread scheduling, connection reuse, and host load
  all vary between runs.
- Capture targets HTTP/1.1 JSON only. Arbitrary streaming, binary, and multipart bodies
  are recorded as metadata and marked unreplayable.
- GhostTraffic shows evidence of a difference. It does not diagnose root causes.
- It does not reverse side effects, so never point it at production.
- A laptop benchmark says nothing about behavior at internet scale.

## Known gaps

- A trace is replayed open-loop with recorded IDs. Requests that depend on an earlier
  response, such as `POST /api/carts/1/items`, can overtake it or hit the wrong row,
  especially above recorded speed. Workflows (Phase 4) address this; until then treat
  those differences as replay artifacts.
- Reports judge p95 response time only, per endpoint.
- A regression must exceed fixed thresholds, so a small real slowdown reads as
  `no_regression`. Confirming one takes at least 5 trials, each with a reset.
- Comparing response bodies needs `--capture-bodies`. Without it, every body reads as
  `body_unavailable`, never as a match.
- `report` loads all results into memory, so very large runs need a matching amount of RAM.
- A trace is held in memory during replay, so its size must fit in RAM.
- Reading a response keeps at most `max_response_bytes`; larger bodies are digested but
  not validated as JSON.
- Secrets inside a URL path, such as `/reset/<token>`, are not detected: only query
  parameters, JSON keys, and headers are sanitized.
- Redacted values replay as the literal `[REDACTED]`, so a request that depends on one
  will likely fail on both targets. Reports show this as `both_failed`.
- Response bodies are not recorded during capture, only status and duration.
- Trace lines are in completion order. Use `arrival_offset_ms` for arrival order.
- A short name resolving into Docker's address pools passes the dial check, and
  redaction cannot catch a secret under an innocent key ([ADR 0005](adr/0005-traffic-safety.md)).
- macOS defaults to a listen backlog of 128 (`kern.ipc.somaxconn`). A burst of more
  new connections than that is reset by the kernel before any server sees it, with or
  without GhostTraffic. Requests lost this way reach no trace, and during replay they
  appear as `connection_reset` results.
- Identical backends can differ by a few times at the median in a single short run, so
  one run is not evidence of a regression. Reports cap it at `observed_regression`.
- Requests in a session can overtake each other by a few milliseconds, even at recorded
  speed. Identical backends then differ very rarely, so the demo verifier allows 0.5% in
  the median trial.
- Laptops stall. Across about 65 demo trials, 2 had an unexplained pause of 400 to 900 ms
  in service time, reaching a p95 of 274 ms. Autovacuum and checkpoints were ruled out by
  logs; the cause is unknown. Reports name such trials, and a verdict needs most trials to
  agree, so a stall cannot create a false alarm unless it hits most of them.
- Sharing a host means small effects can sit below the noise and go undetected.
- Two listeners sharing one database cannot be detected ([ADR 0003](adr/0003-isolated-sequential-replay.md)).
