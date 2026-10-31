# ADR 0004: Open-loop scheduling and two latency measures

**Status:** Accepted

## Context

A tool that waits for a free worker before starting the clock suffers coordinated
omission: when a backend stalls, the requests queued behind the stall never measure
that wait, so the tool under-reports exactly the regression it exists to find.

## Decision

- Replay is open-loop. Each request's due time is fixed in advance as
  `arrival_offset_ms / speed` from one monotonic start, independent of responses.
- Each attempt records `scheduling_delay_ms` (due to actual start) and `duration_ms`
  (actual start to finish).
- Reports carry both **service time** (`latency`) and **response time**
  (`response_time`, delay + duration). Both are stored because quantiles of the
  two parts cannot be added afterwards.
- Quantiles use the exact nearest-rank method over all samples, with no interpolation:
  a reported value is always a value that was observed. Sample counts accompany them,
  and an empty set yields `null`, never zero.
- Throughput is completed requests over wall-clock time and is labeled as such.
- Timeouts stay in the samples at their timeout value, because removing them would
  erase the slowest requests. Drops have no latency, so they are counted and warned.
- `metrics.Assess` gives one of four verdicts:
  - `insufficient_samples`: any trial has under 100 samples.
  - `no_regression`: at most half the trials exceed the thresholds.
  - `observed_regression`: most trials exceed them.
  - `confirmed_regression`: at least 5 paired trials all exceed them (one-sided sign
    test, p = 1/32) and every candidate value is above every baseline value.
- The thresholds (ratio 1.2 and at least 5 ms) are written into each report. Requiring
  both stops sub-millisecond jitter from reading as a large ratio.

## Consequences

Scheduling delay can come from the replayer itself. It is reported per target, with a
warning that does not guess the cause, so a saturated client is not blamed on a backend.
The confirmation rule is a simple sign test, not a model of the latency distribution,
and p95 values from separate trials are its only input.
