# Demo script

The full demo takes about 7 minutes and shows one recorded trace judged three ways.

```sh
colima start           # macOS without Docker Desktop
make regression-demo   # Identical code, deliberate defect, repaired code
open runs/regression-candidate/report.html
make down              # Stop everything and discard its data
```

Each scenario runs 5 trials with a warmup and a database reset before every trial:

1. **Baseline against a twin of itself** must stay quiet on every endpoint.
2. **Baseline against the candidate** must confirm a regression on `GET /api/products`,
   which runs one query per row, and on no other endpoint.
3. **Baseline against the fixed version** must stay quiet again.

Replays use workflows, so identical backends must agree on every response in the median
trial. `verify_report.py` fails the demo if any of this does not happen. `make capture-demo` and
`make replay-demo` run the shorter checks for recording and for replay accuracy.

## Workflows

`make workflow-demo` takes about a minute. It gives one database a different ID range, so
no recorded cart ID exists there. Plain replay fails about half its requests on that target.
With workflows, every checkout completes on both targets at 1x and 2x, which the verifier
confirms from each database's own order count.
