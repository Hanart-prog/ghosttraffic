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

`verify_report.py` fails the demo if any of these does not happen. `make capture-demo` and
`make replay-demo` run the shorter checks for recording and for replay accuracy.
