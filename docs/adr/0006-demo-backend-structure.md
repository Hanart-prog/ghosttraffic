# ADR 0006: Shared demo code with thin variants

**Status:** Accepted

## Context

The demo needs baseline, candidate, and fixed backends with identical routes. Three
full copies would drift, and the regression would be hard to spot among the noise.

## Decision

Shared code lives in `examples/ecommerce/shop/`. Each variant directory holds only
what differs, so the intentional defect is a small, reviewable diff. One Dockerfile
builds any variant through a `VARIANT` build argument.

All variants share one schema and seed, so the planned regression lives in query code
(an N+1 pattern), not a missing index. Differing schemas would conflict with the
requirement for matching migrations, and it is more realistic than an added `sleep`.

## Consequences

A bug in `shop/` affects every variant equally, so it cannot appear as a regression.
