# Limitations

## Current state

Capture and replay work. Report and serve validate their flags and then exit 1, so
results are not yet compared or summarized beyond a one-line summary per target. See the status table in [architecture.md](architecture.md).

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
- Each run does one trial with no warmup, so the first requests are slower on cold
  connections and database pools. Multi-trial runs arrive in Phase 6.
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
- Identical backends can differ by a few times at the median in a single short run.
  In 14 runs of the 121-request demo, 3 showed medians of 8 to 13 ms against the usual
  3 to 4 ms. One run is not evidence of a regression.
- Sharing a host means small effects can sit below the noise and go undetected.
- Two listeners sharing one database cannot be detected ([ADR 0003](adr/0003-isolated-sequential-replay.md)).
