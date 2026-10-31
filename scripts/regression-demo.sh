#!/bin/sh
# Proves the tool in both directions on one recorded trace: it must stay quiet on
# identical code, catch the deliberate defect, and clear the repaired version.
set -eu

trace=traces/regression.jsonl
sent=traces/regression-sent.jsonl
config=examples/ecommerce/replay.yaml
compose=${COMPOSE:-docker compose}
trials=${TRIALS:-5}
sessions=${SESSIONS:-150}
spread=${SPREAD:-10}

mkdir -p traces runs
rm -f "$trace" "$sent"

$compose --profile fixed --profile twin up --build --detach --wait >/dev/null 2>&1
scripts/reset-targets.sh baseline

bin/ghosttraffic capture --listen 127.0.0.1:9000 --upstream http://127.0.0.1:8000 --out "$trace" &
proxy=$!
trap 'kill "$proxy" 2>/dev/null || true' EXIT
tries=0
until nc -z 127.0.0.1 9000 2>/dev/null; do
    tries=$((tries + 1))
    [ "$tries" -lt 50 ] || { echo "proxy did not start" >&2; exit 1; }
    sleep 0.1
done
python3 examples/ecommerce/loadgen.py --url http://127.0.0.1:9000 --out "$sent" \
    --sessions "$sessions" --spread "$spread"
kill -TERM "$proxy"
wait "$proxy"
trap - EXIT
python3 examples/ecommerce/verify_trace.py "$trace" "$sent"

scenario() {
    name=$1
    port=$2
    shift 2
    run=runs/regression-$name
    rm -rf "$run"
    echo "--- baseline vs $name"
    bin/ghosttraffic replay --trace "$trace" --baseline http://127.0.0.1:8000 \
        --candidate "http://127.0.0.1:$port" --config "$config" --trials "$trials" \
        --warmup 200 --capture-bodies --reset-cmd "scripts/reset-targets.sh baseline $name" \
        --out "$run"
    bin/ghosttraffic report --run "$run" --config "$config"
    python3 examples/ecommerce/verify_report.py "$run/report.json" "$@"
}

scenario twin 8003 --expect clean
scenario candidate 8001 --expect regression --endpoint "GET /api/products"
scenario fixed 8002 --expect clean
