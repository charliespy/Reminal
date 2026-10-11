#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# What a viewer opened on a long session is given (client/snapshot_parts.go):
# a session whose history is 4000 syntax-coloured lines (~600 B each), and a
# Playwright viewer opening it fresh. check.mjs counts the distinct lines its
# terminal holds.
#
#   scripts/viewer-snapshot-test/run.sh                   # this tree's relay (viewer) and agent
#   RELAY_BIN=… AGENT_BIN=… scripts/viewer-snapshot-test/run.sh   # any pair of builds (an older viewer, an older agent)
#   WANT=4000 …                                           # fail unless the viewer holds this many
#
# Needs docker; the browser runs in a Playwright container. Nothing here
# touches the host's reminal.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
NAME=${NAME:-reminal-viewer-snapshot-test}
PW=mcr.microsoft.com/playwright:v1.56.0-noble
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/;s/arm64/arm64/')
build() { GOOS=linux GOARCH=$ARCH go build -C "$ROOT" -o "$1" ./cmd/reminal; }
if [ -n "${RELAY_BIN:-}" ]; then cp "$RELAY_BIN" relay; else build "$PWD/relay"; fi
if [ -n "${AGENT_BIN:-}" ]; then cp "$AGENT_BIN" agent; else build "$PWD/agent"; fi
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; docker image rm "$NAME" >/dev/null 2>&1 || true; rm -f "$PWD/relay" "$PWD/agent"' EXIT
docker build -q -t "$NAME" . >/dev/null
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" "$NAME" >/dev/null
docker exec -d "$NAME" sh -c 'reminal-relay relay 18080 >/tmp/relay.log 2>&1'
sleep 2
docker exec "$NAME" sh -c 'cd /root && SHELL=/work.sh reminal new long >/dev/null 2>&1'
for _ in $(seq 120); do docker exec "$NAME" test -f /tmp/answer-done && break; sleep 0.5; done
sleep 2
read -r ID PIN < <(docker exec "$NAME" sh -c 'reminal info long | grep -E "Session:|PIN:" | awk "{print \$2}"' | tr '\n' ' '; echo)
docker run --rm --network "container:$NAME" -v "$PWD:/t:ro" -w /tmp "$PW" sh -c \
  'npm i -s playwright@1.56.0 >/dev/null 2>&1 && cp /t/check.mjs . && node check.mjs "$@"' _ "$ID" "$PIN" "${WANT:-0}"
