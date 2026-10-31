# Demo script

The replay and report walkthrough arrives with Phase 3. Today the demo records synthetic
shop traffic and proves the trace matches it.

```sh
colima start        # macOS without Docker Desktop
make capture-demo   # Start the stack, record traffic, verify the trace
less traces/demo.jsonl
make down           # Stop everything and discard its data
```

The load generator sends 20 concurrent checkout sessions plus edge cases, seeded with
fake secrets. `verify_trace.py` shares no code with the recorder and fails if any fake
secret appears in the trace, or if any request differs from what was sent.
