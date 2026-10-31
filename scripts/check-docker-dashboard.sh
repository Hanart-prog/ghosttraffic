#!/bin/sh
# Proves the container can read run files owned by the host user.
set -eu

compose=${COMPOSE:-docker compose}
HOST_UID=$(id -u) HOST_GID=$(id -g) $compose --profile dashboard up --build --detach dashboard >/dev/null 2>&1
trap '$compose --profile dashboard stop dashboard >/dev/null 2>&1 || true' EXIT

tries=0
until curl -fsS -o /dev/null http://127.0.0.1:7070/health; do
    tries=$((tries + 1))
    [ "$tries" -lt 50 ] || { echo "dashboard did not start" >&2; exit 1; }
    sleep 0.2
done
curl -fsS http://127.0.0.1:7070/ | grep -q '<div id="root">' || { echo "dashboard served no page" >&2; exit 1; }
curl -fsS http://127.0.0.1:7070/api/runs | python3 -c '
import json, sys
runs = json.load(sys.stdin)
judged = [run for run in runs if run["verdict"]]
assert judged, "no run with a readable report"
print(f"ok   container read {len(runs)} runs, {len(judged)} with reports")
'
