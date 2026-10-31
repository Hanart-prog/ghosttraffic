# GhostTraffic

GhostTraffic records HTTP traffic on your laptop, replays it against a baseline and a
candidate backend, and shows evidence of what changed.

> **Status: Phase 0 of 6.** The foundations are built and tested: CLI validation,
> safety configuration, artifact schemas, a demo stack skeleton, and core safeguards
> (egress guard, redaction, open-loop scheduler, regression verdicts). Capture, replay,
> and reporting **do not work yet**; those commands exit 1 and say so.

## Why not just a load tester?

A load tester asks how much a backend can take. GhostTraffic is designed to ask whether
a backend still behaves the same under a recorded workload, by combining:

- replay of recorded arrival patterns, with an open-loop schedule that does not hide stalls
- explicit stateful workflows for flows that depend on generated IDs
- differential comparison of status codes, JSON bodies, and latency distributions

## Build status

| Phase | Scope | State |
| --- | --- | --- |
| 0 | Scaffold, schemas, ADRs, CI | Done |
| 1 | Capture and trace | Planned |
| 2 | Replay engine | Planned |
| 3 | Comparison and reports | Planned |
| 4 | Stateful workflows | Planned |
| 5 | Local dashboard | Planned |
| 6 | Hardening and benchmarks | Planned |

## Getting started

You need Go 1.24+ and Python 3.12+. The demo stack also needs Docker with Compose.

```sh
make build   # Build bin/ghosttraffic
make test    # Go tests with the race detector, plus demo backend tests
make lint    # gofmt, go vet, staticcheck, ruff
make demo    # Start the stack and check that both backends share seed state
make down    # Stop the stack and discard its data
```

The CLI's commands are stable, even though most engines are not built:

```sh
bin/ghosttraffic capture --listen 127.0.0.1:9000 --upstream http://localhost:8000 --out traces/demo.jsonl
bin/ghosttraffic replay --trace traces/demo.jsonl --baseline http://localhost:8000 \
  --candidate http://localhost:8001 --concurrency 50 --speed 1 --out runs/run-001
bin/ghosttraffic report --run runs/run-001 --format html,json
bin/ghosttraffic serve --runs-dir runs --listen 127.0.0.1:7070
```

## Safety

Replay targets default to loopback and Compose service names. Any other host needs an
explicit `--allow-host` and prints a warning. Only allowlisted headers are stored, and
credentials cannot be allowlisted. Use synthetic traffic only, and never point replay
at production: it sends real mutating requests.

## Learn more

- [Architecture and risks](docs/architecture.md)
- [Implementation plan](docs/implementation-plan.md)
- [Limitations](docs/limitations.md)
- [Decision records](docs/adr/)

## License

[MIT](LICENSE)
