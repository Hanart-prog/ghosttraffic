# ADR 0009: A read-only API that trusts nothing

**Status:** Accepted

## Context

The dashboard needs run data over HTTP. A service on localhost is still reachable from
any web page the user visits, and run directories hold sanitized but real traffic.

## Decision

- The API only reads files, and rejects every method except GET and HEAD.
- It binds to loopback by default, and warns on any other address, because the `Host`
  check stops web pages but not someone on the network who can set any header.
- It rejects any `Host` that is not localhost, which stops DNS rebinding. CORS does not:
  after rebinding, the browser treats the page as same-origin.
- CORS is off unless an origin is named with `--allow-origin`.
- Run IDs must match a strict pattern, and run directories and artifacts must not be
  symlinks, so a planted link cannot expose other files.
- Request paging uses byte offsets into `results.jsonl`, so the server holds no state and
  deep pages cost the same as early ones.
- Replay keeps a copy of its sanitized requests as `trace.jsonl`, so a run can explain
  itself after the original trace is gone.
- The dashboard derives nothing. Numbers come from `report.json` and the results, and
  absent values display as "n/a" or "not available".

## Consequences

Request detail and the timeline scan the results file on each call, which is fine for
local runs of this size. The container needs `--listen 0.0.0.0` inside its own network,
so Compose publishes it on host loopback only.
