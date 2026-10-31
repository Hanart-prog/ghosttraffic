# Limitations

## Current state

Capture, replay, report, and serve validate their flags and then exit 1. No traffic is
recorded or replayed yet. See the status table in [architecture.md](architecture.md).

## By design

- Replay is not fully deterministic. Thread scheduling, connection reuse, and host load
  all vary between runs.
- Capture targets HTTP/1.1 JSON only. Arbitrary streaming, binary, and multipart bodies
  are recorded as metadata and marked unreplayable.
- GhostTraffic shows evidence of a difference. It does not diagnose root causes.
- It does not reverse side effects, so never point it at production.
- A laptop benchmark says nothing about behavior at internet scale.

## Known gaps

- The safeguards in [architecture.md](architecture.md) are not yet driven by any engine.
- A short name resolving into Docker's address pools passes the dial check, and
  redaction cannot catch a secret under an innocent key ([ADR 0005](adr/0005-traffic-safety.md)).
- Sharing a host means small effects can sit below the noise and go undetected.
- Two listeners sharing one database cannot be detected ([ADR 0003](adr/0003-isolated-sequential-replay.md)).
