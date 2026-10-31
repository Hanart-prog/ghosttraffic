#!/bin/sh
# Replays traces/demo.jsonl against freshly seeded baseline and candidate backends
# at 1x and 2x speed, and checks each run against the backends' access logs.
# Expects make capture-demo to have left the stack running.
set -eu

trace=traces/demo.jsonl
baseline=http://127.0.0.1:8000
candidate=http://127.0.0.1:8001
compose=${COMPOSE:-docker compose}

mkdir -p runs
rm -rf runs/demo-1x runs/demo-2x

# Capture changed the baseline's data, so a replay now must be refused.
if bin/ghosttraffic replay --trace "$trace" --baseline "$baseline" --candidate "$candidate" \
    --out runs/demo-refused 2>runs/refused.log; then
    echo "FAIL replay ran on backends with different state" >&2
    exit 1
fi
grep -q "different seed state" runs/refused.log
echo "ok   replay refused while the backends' data differed"
rm -f runs/refused.log

replay_at() {
    speed=$1
    run=runs/demo-${speed}x
    $compose down --volumes --remove-orphans >/dev/null 2>&1
    $compose up --detach --wait >/dev/null 2>&1
    bin/ghosttraffic replay --trace "$trace" --baseline "$baseline" --candidate "$candidate" \
        --config examples/ecommerce/replay.yaml --speed "$speed" --concurrency 50 --out "$run"
    $compose logs --no-log-prefix baseline >"$run/baseline.log" 2>&1
    $compose logs --no-log-prefix candidate >"$run/candidate.log" 2>&1
    python3 examples/ecommerce/verify_run.py "$trace" "$run" "$run/baseline.log" "$run/candidate.log"
}

replay_at 1
replay_at 2
