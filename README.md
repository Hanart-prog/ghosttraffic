# GhostTraffic

GhostTraffic records HTTP traffic on your laptop, replays it against a baseline and a
candidate backend, and shows evidence of what changed.

> **Status: Phase 5 of 6.** Capture, replay, comparison reports, stateful workflows, and a
> local dashboard work. On one recorded trace the demo stays quiet on identical code,
> confirms a deliberate N+1 query on the right endpoint, and clears the repaired version.

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
| 1 | Capture and trace | Done |
| 2 | Replay engine | Done |
| 3 | Comparison and reports | Done |
| 4 | Stateful workflows | Done |
| 5 | Local dashboard | Done |
| 6 | Hardening and benchmarks | Planned |

## Getting started

You need Go 1.24+, Python 3.12+, and Node 20+. The demo stack also needs Docker with Compose.

```sh
make build   # Build bin/ghosttraffic
make test    # Go tests with the race detector, plus demo backend tests
make lint    # gofmt, go vet, staticcheck, ruff
make demo    # Start the stack and check that both backends share seed state
make capture-demo  # Record synthetic traffic and verify the trace against it
make replay-demo   # Then replay it against both backends and verify the run
make regression-demo  # Identical code, deliberate defect, repaired code, about 7 minutes
make workflow-demo    # Checkouts on a target whose generated IDs differ from the recording
make dashboard  # Browse runs at http://127.0.0.1:7070
make image   # Build the CLI and dashboard container image
make down    # Stop the stack and discard its data
```

On macOS, Colima provides Docker without Docker Desktop: run `colima start` first.

Every command works today:

```sh
bin/ghosttraffic capture --listen 127.0.0.1:9000 --upstream http://localhost:8000 --out traces/demo.jsonl
bin/ghosttraffic validate --trace traces/demo.jsonl
bin/ghosttraffic replay --trace traces/demo.jsonl --baseline http://localhost:8000 \
  --candidate http://localhost:8001 --concurrency 50 --speed 1 --out runs/run-001 \
  --trials 5 --warmup 200 --capture-bodies --reset-cmd 'scripts/reset-targets.sh baseline candidate'
bin/ghosttraffic report --run runs/run-001 --format html,json
bin/ghosttraffic serve --runs-dir runs --listen 127.0.0.1:7070
```

## Safety

Replay targets default to loopback and Compose service names. Any other host needs an
explicit `--allow-host` and prints a warning, and sending writes to it also needs
`--allow-external-mutation`. Replay refuses targets reporting different seed state. Only allowlisted headers are stored, and
credentials cannot be allowlisted. Use synthetic traffic only, and never point replay
at production: it sends real mutating requests.

## Learn more

- [Architecture and risks](docs/architecture.md)
- [Implementation plan](docs/implementation-plan.md)
- [Limitations](docs/limitations.md)
- [Decision records](docs/adr/)

## License

[MIT](LICENSE)
