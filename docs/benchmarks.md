# Benchmarks

These numbers describe GhostTraffic itself, on one laptop, against a trivial target. They
say nothing about how any real service performs. Run `make bench` to measure your own host.

## Environment

MacBook Pro, Apple M3 Pro (12 cores), 36 GB RAM, macOS 14 (Darwin 23.2.0), Go 1.27.3, on the
on AC power with the lid closed, default power profile. No sleep events occurred during the
run, checked against `pmset -g log`.

## Method

`tests/benchmark` drives the built binary from outside, as a user would. Each row is the
median of 3 repetitions. Targets are Go HTTP servers running on the same machine, so the
load generator and the target share CPU, and loopback hides real network cost.

- **Replay pacing** generates a 5-second trace at each rate and replays it against targets
  that sleep 1 ms, with `--concurrency 500`. Offered rate is what the trace asks for.
  Achieved rate and delay come from `results.jsonl` for the baseline pass. Trace times are
  whole milliseconds, so rates above 1,000 req/s arrive in small bursts.
- **Capture overhead** sends 40,000 requests in a closed loop to a target with no delay,
  directly and through `capture`, over keep-alive connections.
- **Report cost** builds HTML and JSON from a 100,000-result run, without stored bodies.
  Peak memory is the process's maximum resident set.

## Results

### Replay pacing

| Offered req/s | Achieved req/s | Delay p50 ms | Delay p99 ms | Delay max ms | Errors |
| --- | --- | --- | --- | --- | --- |
| 500 | 500 | 0.14 | 0.17 | 0.7 | 0 |
| 1,000 | 1,000 | 0.19 | 0.21 | 0.2 | 0 |
| 2,000 | 2,000 | 0.20 | 0.22 | 0.6 | 0 |
| 5,000 | 5,001 | 0.21 | 0.25 | 0.5 | 0 |
| 10,000 | 10,002 | 0.20 | 0.28 | 1.0 | 0 |

10,000 req/s is the highest rate tested, not a limit that was found. An earlier lid-open run,
before the Go language version moved to 1.26, measured p50 delay of 0.10 to 0.14 ms. The
cause of that difference was not isolated, so treat tenths of a millisecond as host noise.

### Capture overhead

| Connections | Path | p50 ms | p99 ms | req/s |
| --- | --- | --- | --- | --- |
| 1 | direct | 0.028 | 0.039 | 34,366 |
| 1 | through capture | 0.060 | 0.143 | 15,678 |
| 16 | direct | 0.063 | 0.401 | 180,797 |
| 16 | through capture | 0.386 | 1.132 | 37,778 |

Capture adds about 0.03 ms at the median on one connection and 0.3 ms at 16. That is small
beside a real request but is not zero, so a trace's recorded durations include it.

### Report cost

| Results | File MB | Seconds | Peak RSS MB |
| --- | --- | --- | --- |
| 100,000 | 33.9 | 0.46 | 214 |

Stored response bodies make results files much larger: the demo's 9,010 results with
bodies take about 12 MB per run, so memory should be judged against file size too.

## What the benchmark found

The first run of the 16-connection capture test failed with local port exhaustion. Capture
set no connection limit, so Go kept only 2 idle upstream connections and redialed the rest.
It now keeps a pool, and `TestUnlimitedClientKeepsAPool` guards the fix.

## What this does not show

- Behavior beyond 5 seconds per rate, or under sustained memory pressure.
- Real networks, TLS, or slow targets at high concurrency.
- Linux. These numbers are from macOS only.
