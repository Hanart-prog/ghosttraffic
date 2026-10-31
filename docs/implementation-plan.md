# Implementation plan

Each phase ends with its gate passing and stops for review. The master specification
remains the source of requirements; this file maps them to deliverables.

| Phase | Deliverables | Acceptance tests |
| --- | --- | --- |
| 0. Scaffold | CLI shell, `pkg/config`, schemas, demo skeleton, Compose, Makefile, CI, ADRs | `make build`, `make test`, `make lint`; CI runs the stack |
| 1. Capture | `internal/capture`, trace writer and reader, synthetic load generator | Trace reproduces sanitized requests; upstream response is byte-identical; secrets are absent; memory stays flat on oversized bodies |
| 2. Replay | `internal/replay`, dial-time guard, no-redirect client, results writer | Fake-clock arrival tests at 1x and 2x; atomic in-flight counter never exceeds the cap; every request gets a terminal result; both targets get equal logical workloads |
| 3. Compare and report | `internal/compare`, `internal/analysis`, `internal/report`, trials, warmup, candidate defect, `make regression-demo` | Identical code stays quiet; demo detects the N+1 defect; `fixed` improves within tolerance; absent bodies read "not available"; both-failed is not a match; same inputs give same bytes |
| 4. Workflows | `internal/workflow`, YAML format, JSON-path subset | Cycles and missing variables rejected; downstream steps stop after a failure; cart to order flow works per target under concurrent sessions |
| 5. Dashboard | `internal/server`, `web/` pages | Path traversal rejected; API bound to loopback; pagination stable; every chart traces to report JSON; no dead controls |
| 6. Hardening | Uncertainty indicators, benchmarks, security tests, demo script, README media | Clean-clone `make demo`; measured benchmarks with methodology; docs match behavior |

## Left as is

The dashboard bundle is over 500 kB, mostly the chart library. That is acceptable for a
local tool; split it only if load time is measurably a problem.
