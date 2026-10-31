# Implementation plan

Each phase ends with its gate passing and stops for review. The master specification
remains the source of requirements; this file maps them to deliverables.

| Phase | Deliverables | Acceptance tests |
| --- | --- | --- |
| 0. Scaffold | CLI shell, `pkg/config`, schemas, demo skeleton, Compose, Makefile, CI, ADRs | `make build`, `make test`, `make lint`; CI runs the stack |
| 1. Capture | `internal/capture`, trace writer and reader, synthetic load generator | Trace reproduces sanitized requests; upstream response is byte-identical; secrets are absent; memory stays flat on oversized bodies |
| 2. Replay | `internal/replay`, dial-time guard, no-redirect client, results writer | Fake-clock arrival tests at 1x and 2x; atomic in-flight counter never exceeds the cap; every request gets a terminal result; both targets get equal logical workloads |
| 3. Compare and report | `internal/compare`, metrics aggregation, `internal/report`, candidate defect, full `make demo` | Demo detects the N+1 defect; `fixed` improves within tolerance; absent bodies read "not available"; both-failed is not a match; same inputs give same bytes |
| 4. Workflows | `internal/workflow`, YAML format, JSON-path subset | Cycles and missing variables rejected; downstream steps stop after a failure; cart to order flow works per target under concurrent sessions |
| 5. Dashboard | `internal/server`, `web/` pages | Path traversal rejected; API bound to loopback; pagination stable; every chart traces to report JSON; no dead controls |
| 6. Hardening | Trials and alternation, uncertainty indicators, benchmarks, security tests, demo script, README media | Clean-clone `make demo`; measured benchmarks with methodology; docs match behavior |

## Carried into Phase 2

- Add a container image for the CLI, so a host without Go can run the full demo.
- Classify connection resets explicitly: on macOS, concurrency above 128 can trigger
  them from the listen backlog alone (see limitations), which must not read as a backend fault.
- Send requests inside the `replay.Schedule` handler with the `egress` client, writing
  one `metrics.Result` per slot, including drops and cancellations.
- Call `replay.SeedDigests` before the first pass and record both digests in `run.json`.

## Carried into Phase 3

- Build reports from `metrics.Stats`, `metrics.Assess`, `metrics.Warnings`,
  `compare.Classify`, and `compare.Warnings`.
- Pipe canonical, ignore-path-filtered digests into `Classify`.
