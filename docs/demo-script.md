# Demo script

The report walkthrough arrives with Phase 3. Today the demo records synthetic shop
traffic, then replays it against both backends.

```sh
colima start       # macOS without Docker Desktop
make replay-demo   # Record, verify, reset, replay at 1x and 2x, verify
less traces/demo.jsonl
less runs/demo-1x/results.jsonl
make down          # Stop everything and discard its data
```

`verify_trace.py` fails if a fake secret reaches the trace or a request differs from what
was sent. `verify_run.py` compares results with each backend's own access log, which
GhostTraffic does not write. At 2x the demo deliberately exceeds what the single-process
backends can serve, so response times and scheduling delay rise, and some requests
overtake the cart they depend on. Those are real results, not errors in the replay.
