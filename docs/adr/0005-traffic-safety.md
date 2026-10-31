# ADR 0005: Safe-by-default capture and egress

**Status:** Accepted

## Context

Traces are copies of real traffic, and replay sends mutating requests. The dangerous
failures are a credential in a trace and a replay that reaches production.

## Decision

- **Headers are allowlisted**, not denylisted: only `Accept` and `Content-Type` are
  stored by default. Sensitive headers cannot be allowlisted, and `trace.Validate`
  rejects them on read, so a hand-edited trace cannot reintroduce them.
- **Unknown config keys are errors**, so a misspelled safety setting fails loudly.
- **Targets**: loopback and single-label Compose service names pass. Anything else
  must be named exactly by `--allow-host` or `replay.allow_hosts`, and warns.
- A trace stores no absolute URL, and paths must be origin-form, so `//host/x`
  cannot change the target when joined to a base URL.
- A truncated body is never replayable.

## Enforcement at the network

`CheckTarget` inspects names only, so `internal/egress` enforces policy on the
address each connection actually resolves to:

- Loopback is always permitted.
- Link-local (including cloud metadata), multicast, and unspecified addresses are
  always refused, even for allowlisted hosts.
- Allowlisted hosts may reach any other address.
- Single-label names may reach Docker's default pools only: `172.16.0.0/12` and
  `192.168.0.0/16`. `10.0.0.0/8` is excluded as the usual corporate range.
- Redirects are returned, not followed, and proxy environment variables are ignored,
  since a proxy would carry traffic past the dial check.

A boundary test fails the build if code outside `internal/egress` constructs its own
HTTP client or transport.

Traffic is sanitized in `internal/capture` before it reaches a trace: sensitive JSON
keys and query parameters are redacted by name at any depth, even with an empty
config, and configured paths are redacted on top.

Non-loopback Docker addresses such as `172.18.0.2` given as literals still need
`--allow-host`, because the name check cannot tell them from other private hosts.

**Residual risk:** a short name such as `prod` that resolves into `172.16.0.0/12` or
`192.168.0.0/16` passes. A LAN host in those ranges cannot be told from a container.
Name-based redaction cannot catch a secret stored under an innocent key.

## Consequences

Allowing a host is a human
assertion that it is safe, which this tool cannot verify.
