# Limitations

## Current state

Every command works. The dashboard is local-only, with no accounts or sharing. See the
status table in [architecture.md](architecture.md) and measured numbers in
[benchmarks.md](benchmarks.md).

## By design

- Replay is not fully deterministic. Thread scheduling, connection reuse, and host load
  all vary between runs.
- Capture targets HTTP/1.1 JSON only. Arbitrary streaming, binary, and multipart bodies
  are recorded as metadata and marked unreplayable.
- GhostTraffic shows evidence of a difference. It does not diagnose root causes.
- It does not reverse side effects, so never point it at production.
- A laptop benchmark says nothing about behavior at internet scale. Benchmarks here are
  for the tool, against a trivial target, and do not predict any real service.

## Known gaps

- Without a workflow, requests replay open-loop with recorded IDs, so a request that
  depends on an earlier response can overtake it or hit the wrong row. Workflows fix this
  only for sessions someone has described, and nothing is inferred automatically.
- A workflow file names recorded request IDs, so it belongs to one trace.
- Workflow steps wait for their parents, so a slow target receives children later. Read
  `dependency_wait_ms` beside scheduling delay.
- JSON paths support `$.a.b` and `$.items[*].id`; extraction takes exactly one value.
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
- Laptops stall. Across about 65 demo trials, 2 had an unexplained pause of 400 to 900 ms
  in service time, reaching a p95 of 274 ms. Autovacuum and checkpoints were ruled out by
  logs; the cause is unknown. Reports name such trials, and a verdict needs most trials to
  agree, so a stall cannot create a false alarm unless it hits most of them.
- Generated `report.html` files use the same colors but system fonts, because they stay
  standalone, and follow the system theme with no switch.
- Colima shares only your home directory with containers by default. A repo elsewhere,
  such as `/tmp`, gets empty mounts: the databases start unseeded and the stack never turns
  healthy.
- `make test-browser` needs Chrome installed and the runs that `make demo` produces.
- A laptop that sleeps during a run pauses it, and the trial spanning the sleep is not
  meaningful. On macOS, run `caffeinate -i make demo` with the lid open: `caffeinate` does
  not keep a closed laptop awake on its own.
- Sharing a host means small effects can sit below the noise and go undetected.
- Two listeners sharing one database cannot be detected ([ADR 0003](adr/0003-isolated-sequential-replay.md)).
