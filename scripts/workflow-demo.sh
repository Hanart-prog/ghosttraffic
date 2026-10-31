#!/bin/sh
# Proves workflows on a target whose generated IDs differ from the recording: the
# plain replay must fail there, and the workflow replay must finish every checkout.
set -eu

trace=traces/workflow.jsonl
sent=traces/workflow-sent.jsonl
workflows=traces/workflow.yaml
config=examples/ecommerce/replay.yaml
compose=${COMPOSE:-docker compose}
sessions=${SESSIONS:-40}
shop="$compose --profile twin exec -T postgres-twin psql -q -U shop -d shop"

mkdir -p traces runs
rm -f "$trace" "$sent" "$workflows"
rm -rf runs/workflow-plain runs/workflow-1x runs/workflow-2x

if ! log=$($compose --profile twin up --build --detach --wait baseline twin 2>&1); then
    echo "$log" >&2
    echo "The stack did not start. Under Colima, the repo must be inside your home directory." >&2
    exit 1
fi
scripts/reset-targets.sh baseline

bin/ghosttraffic capture --listen 127.0.0.1:9000 --upstream http://127.0.0.1:8000 --out "$trace" 2>/dev/null &
proxy=$!
trap 'kill "$proxy" 2>/dev/null || true' EXIT
tries=0
until nc -z 127.0.0.1 9000 2>/dev/null; do
    tries=$((tries + 1))
    [ "$tries" -lt 50 ] || { echo "proxy did not start" >&2; exit 1; }
    sleep 0.1
done
python3 examples/ecommerce/loadgen.py --url http://127.0.0.1:9000 --out "$sent" \
    --sessions "$sessions" --spread 4
kill -TERM "$proxy"
wait "$proxy"
trap - EXIT
python3 examples/ecommerce/make_workflows.py "$trace" "$workflows"

fresh() {
    scripts/reset-targets.sh baseline twin
    # Same seed, so the digests still match, but no recorded cart ID exists here.
    $shop -c "ALTER SEQUENCE carts_id_seq RESTART 500000; ALTER SEQUENCE orders_id_seq RESTART 500000;"
}

replay_run() {
    bin/ghosttraffic replay --trace "$trace" --baseline http://127.0.0.1:8000 \
        --candidate http://127.0.0.1:8003 --config "$config" "$@" 2>/dev/null
}

facts() {
    for db in postgres-baseline postgres-twin; do
        $compose --profile twin exec -T "$db" psql -qtA -F ' ' -U shop -d shop \
            -c "SELECT count(*), coalesce((SELECT min(id) FROM carts), 0) FROM orders;"
    done >"$1"
}

echo "--- recorded IDs, no workflows"
fresh
replay_run --out runs/workflow-plain

for speed in 1 2; do
    echo "--- workflows at ${speed}x"
    fresh
    replay_run --workflows "$workflows" --speed "$speed" --out "runs/workflow-${speed}x"
    facts "runs/workflow-${speed}x/db-facts.txt"
    python3 examples/ecommerce/verify_workflows.py "runs/workflow-${speed}x" "$sessions" \
        runs/workflow-plain "runs/workflow-${speed}x/db-facts.txt"
done
