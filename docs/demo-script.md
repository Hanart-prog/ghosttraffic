# Demo script

The full capture, replay, and report walkthrough arrives with Phase 3. Today the demo
starts the stack and proves both backends began from the same data.

```sh
make demo    # Start baseline and candidate, then compare seed digests
curl -s http://127.0.0.1:8000/health
curl -s http://127.0.0.1:8001/health
make down    # Stop everything and discard its data
```
