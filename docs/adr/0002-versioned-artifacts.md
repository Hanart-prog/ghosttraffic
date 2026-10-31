# ADR 0002: Versioned, strictly parsed artifacts

**Status:** Accepted

## Context

Traces, results, and reports outlive the binary that wrote them. A reader that
guesses at an unfamiliar document can produce a plausible but wrong report.

## Decision

- Every artifact carries `schema_version`. All are decoded by `internal/schema`.
- The version is checked before the body, so a future file reads "unsupported
  version", not "unknown field".
- Unknown fields and trailing data are errors. New fields require a version bump.
- Traces and results are JSONL: append-only, one independently parseable record per
  line, so a crash loses at most the last line.
- Reports use slices rather than maps, and struct order fixes key order, so equal
  inputs produce equal bytes. Golden-file tests hold this in place.
- Fields a later phase needs are defined now where adding them later would force a
  version bump: `trial` on results and `response_time` on reports.

## Consequences

Old binaries refuse new artifacts instead of misreading them. This is intended.
