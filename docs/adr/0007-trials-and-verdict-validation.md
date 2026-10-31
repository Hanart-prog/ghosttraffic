# ADR 0007: Repeated trials, and proving the verdicts

**Status:** Accepted

## Context

Single short replays of identical backends differed by up to 40 times while a demo
connection pool warmed up, and by about 3 times after it was fixed. No threshold can
separate a regression from noise on one run, and a report nobody tests for false alarms
is only an opinion.

## Decision

- Multiple trials arrived in Phase 3, not Phase 6, because verdicts depend on them.
- Each trial resets both targets through `--reset-cmd`, and replay fails if the seed
  digest does not return to its first value. A trace with writes cannot run more than
  one trial without a reset.
- Each pass starts with a warmup of read-only requests from the trace, never recorded.
  Writes are excluded because they would change the state about to be measured.
- Verdicts use p95 response time, per endpoint, per trial.
- Comparison uses canonical JSON from stored bodies, with ignored paths listed in the
  report. Raw digests are not compared, because ignored paths cannot apply to them.
- The demo is the test: the same trace must read as clean against a twin of the baseline,
  as confirmed on exactly one endpoint against the defect, and as clean against the repair.
- Rare stalls are expected, so reports name a trial more than 3 times above its target's
  median, and the demo judges response differences on the median trial, like verdicts.
- Report charts share one axis, so noise cannot be drawn as large as a real regression.

## Consequences

The full demo takes minutes rather than seconds. Reset commands are arbitrary shell run
by the operator, and are trusted as such. The false-alarm test covers this demo on this
kind of host; other systems need their own identical-code calibration run.
