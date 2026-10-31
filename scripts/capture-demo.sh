#!/bin/sh
# Records synthetic traffic through the proxy into traces/demo.jsonl, then checks
# the trace against what was sent. Expects the demo stack to be running.
set -eu

trace=traces/demo.jsonl
sent=traces/demo-sent.jsonl
addr=127.0.0.1:9000

mkdir -p traces
rm -f "$trace" "$sent"

bin/ghosttraffic capture --listen "$addr" --upstream http://127.0.0.1:8000 --out "$trace" &
proxy=$!
trap 'kill "$proxy" 2>/dev/null || true' EXIT

tries=0
# Probe the port, not a URL: a request would become part of the recorded workload.
until nc -z 127.0.0.1 9000 2>/dev/null; do
    tries=$((tries + 1))
    [ "$tries" -lt 50 ] || { echo "proxy did not start" >&2; exit 1; }
    sleep 0.1
done

python3 examples/ecommerce/loadgen.py --url "http://$addr" --out "$sent"

kill -TERM "$proxy"
wait "$proxy"
trap - EXIT

bin/ghosttraffic validate --trace "$trace"
python3 examples/ecommerce/verify_trace.py "$trace" "$sent"
