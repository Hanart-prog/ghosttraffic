# ADR 0003: Replay each target separately, against its own state

**Status:** Accepted

## Context

Mirroring each request to both backends at once is the common shadow-traffic design.
With mutating requests it is wrong if they share a database: the second `POST /orders`
sees the first one's effects, so the "difference" is an artifact of the method.

## Decision

- Each target gets its own independently seeded database.
- A run executes the same logical workload twice, once per target, not simultaneously.
  The two targets therefore never compete for the same CPU during a measurement.
- Results join on `(trial, request_id)`, never on runtime IDs or completion order.
- Multi-trial runs (v1) alternate target order to reduce warm-cache and order bias.
- Demo databases live on tmpfs, so restarting returns to the seed. Backends report a
  `seed_digest` from `/health`, and `make stack` fails unless the digests match.

## Consequences

- A run takes twice as long as the workload, and the two passes are not simultaneous,
  so slow drift on the host appears as a difference. Alternation and multiple trials
  reduce this but do not remove it.
- `SameBackend` rejects one listener given twice, including loopback respellings. It
  cannot detect two listeners sharing a database; that remains the operator's duty.
- Recorded runtime IDs do not exist on a fresh target, so a stateless replay of
  `GET /carts/17` may return 404 on both sides. `internal/compare` therefore classifies
  equal failures as `both_failed` and uncaptured bodies as `body_unavailable`, adding a
  warning for each. Neither counts as `match`. Stateful flows use explicit workflows.
- Before a run, `replay.SeedDigests` refuses targets reporting different seed state. A
  target with no digest leaves equal state unknown, and `run.json` shows it as absent.
