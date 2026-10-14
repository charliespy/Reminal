#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The viewer reconnecting, on a phone's link (tc netem: DELAY each way on
# the relay's loopback, RATE): the real viewer, served by a real relay,
# against a real session, in a container; the browser in a Playwright one.
#
#   1. first open: the PIN handshake's library is fetched before the socket
#      opens, not once auth_ok asks for it
#   2. a dropped connection that was up is dialled again at once; a redial
#      that then fails waits the backoff
#   3. a PIN handshake that was told "busy" and then went through says the
#      session is connected, not "Busy — trying again"
#   4. after a reconnect, what is typed echoes on the prompt's line
#
#   scripts/viewer-reconnect-test/run.sh [TREE]   # TREE: a checkout to test (default: this one)
# Needs docker. Nothing here touches the host's reminal.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
TREE=${1:-$ROOT}
DELAY=${DELAY:-150ms} RATE=${RATE:-1600kbit}
NAME=reminal-viewer-reconnect-test
PW=mcr.microsoft.com/playwright:v1.56.0-noble
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/;s/arm64/arm64/')
docker run --rm -v "$TREE":/src:ro -v "$PWD":/out -e GOFLAGS=-buildvcs=false golang:1.26-bookworm \
  sh -c "cp -r /src /w && cd /w && GOOS=linux GOARCH=$ARCH go build -o /out/reminal ./cmd/reminal"
docker build -q -t "$NAME" . >/dev/null
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" --cap-add NET_ADMIN "$NAME" >/dev/null
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; docker rmi -f "$NAME" >/dev/null 2>&1 || true; rm -f "$PWD/reminal"' EXIT
docker exec -d "$NAME" sh -c 'reminal relay 18080 >/tmp/relay.log 2>&1'
sleep 3
docker cp tui.sh "$NAME":/root/tui.sh >/dev/null
docker exec "$NAME" sh -c 'cd /root && reminal new alpha >/dev/null 2>&1 && SHELL=/root/tui.sh reminal new tui >/dev/null 2>&1'
creds=$(docker exec "$NAME" sh -c 'for s in alpha tui; do reminal info $s | grep -E "Session:|PIN:" | awk "{print \$2}"; done' | tr '\n' ' ')
echo "session: $creds   link: $DELAY each way, $RATE"
docker exec "$NAME" tc qdisc add dev lo root netem delay "$DELAY" rate "$RATE"
docker run --rm --network "container:$NAME" -v "$PWD:/t" -w /tmp "$PW" sh -c \
  'npm i -s playwright@1.56.0 >/dev/null 2>&1 && cp /t/checks.mjs . && node checks.mjs "$@"' _ $creds
