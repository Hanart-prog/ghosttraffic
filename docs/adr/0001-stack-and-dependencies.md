# ADR 0001: Stack and dependency policy

**Status:** Accepted

## Context

GhostTraffic must run on a laptop with no accounts, and its measurements must be
trustworthy. Every dependency in the replay path is a possible source of latency
that gets attributed to the system under test.

## Decision

- **Go** for the CLI, recorder, replay engine, analysis, and local API: precise
  control of timers, connection pools, and goroutine bounds, plus a race detector.
- **Standard library first.** `yaml.v3` is the only Go dependency so far. New ones
  need a reason recorded here or in the PR.
- **JSONL and JSON files are canonical.** No database is required; SQLite stays
  optional and may only ever be an index over those files.
- **Python/FastAPI and PostgreSQL only in the demo**, as the system under test.
- **React dashboard over a read-only API.** The engine and CLI never depend on it.
- No Kafka, Redis, Kubernetes, or telemetry stack: no measured need exists.

## Consequences

Large runs are scanned, not queried, so the API paginates by file offset. If that
proves too slow it can be measured and revisited.
