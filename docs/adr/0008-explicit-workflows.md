# ADR 0008: Explicit stateful workflows

**Status:** Accepted

## Context

Replaying recorded IDs breaks on any target that generates different ones, and open-loop
requests can overtake the request they depend on. Inferring dependencies from traffic
would be a guess, and a wrong guess would corrupt the comparison without any sign.

## Decision

- Dependencies, extraction, and substitution are declared in a workflow file. Nothing is
  inferred. The demo generates its file with a script that knows the shop's routes, and
  the output is an ordinary workflow file.
- A file is validated against the trace before anything is sent: cycles, unknown steps or
  requests, variables no dependency extracts, and substitutions that do not apply.
- A step is ready at the later of its recorded time and its last parent's finish. That
  wait is `dependency_wait_ms`, separate from scheduling delay and latency, so a slow
  parent is not charged to its child.
- Variables exist per pass and per session, so a value from one target cannot reach the other.
- Extracted values come from a target, so they are escaped before entering a URL path.
- After a failed prerequisite, a step sends nothing and records `dependency_failed`, unless
  the workflow sets `on_dependency_failure: continue`.
- Sessions without a workflow stay fully open-loop.
- Extraction reads responses in memory even without `--capture-bodies`. Nothing extracted
  is written to disk.

## Consequences

Workflow steps are partly closed-loop: a slow parent delays its children, which lowers the
load on a slow target. Scheduling delay no longer captures that for those requests, so
`dependency_wait_ms` must be read alongside it. A workflow file belongs to one trace,
because it names recorded request IDs.
