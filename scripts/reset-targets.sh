#!/bin/sh
# Recreates the named backends and their databases from the seed.
set -eu

compose=${COMPOSE:-docker compose}
profiles="--profile fixed --profile twin"
services=
for name in "$@"; do
    services="$services $name postgres-$name"
done
# shellcheck disable=SC2086
$compose $profiles rm --stop --force --volumes $services >/dev/null 2>&1
# shellcheck disable=SC2086
if ! log=$($compose $profiles up --detach --wait "$@" 2>&1); then
    echo "$log" >&2
    exit 1
fi
