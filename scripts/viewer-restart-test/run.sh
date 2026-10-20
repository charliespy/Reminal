#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A page left open on a session whose process ended and came back as itself
# (`reminal restore`: same id, its scrollback replayed). The real viewer,
# served by a real relay, against a real session, in a container; the browser
# in a Playwright one. What is typed once it is back echoes:
#   1. back from a record as new as the page's screen
#   2. back from a record a few frames older (what it said after its last
#      save, lost with the process)
#   3. and its screen is drawn again
#
#   scripts/viewer-restart-test/run.sh [TREE]   # TREE: a checkout to test (default: this one)
# BIG=1 (with CPUS=0.25, a basic machine's share): only a measurement — a
# 300,000-line scrollback restarted, and when the page has its key against
# when the restored screen is drawn (a handshake must not wait for the
# replay).
# Needs docker. Nothing here touches the host's reminal.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
TREE=${1:-$ROOT}
# Its container and image, one of this run's own (NAME to choose it), so two
# runs at once never remove each other's.
NAME=${NAME:-reminal-viewer-restart-test-$$}
PW=mcr.microsoft.com/playwright:v1.56.0-noble
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/;s/arm64/arm64/')
docker run --rm -v "$TREE":/src:ro -v "$PWD":/out -e GOFLAGS=-buildvcs=false golang:1.26-bookworm \
  sh -c "cp -r /src /w && cd /w && GOOS=linux GOARCH=$ARCH go build -o /out/reminal ./cmd/reminal"
docker build -q -t "$NAME" . >/dev/null
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --init ${CPUS:+--cpus "$CPUS"} --name "$NAME" "$NAME" >/dev/null
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; docker rmi -f "$NAME" >/dev/null 2>&1 || true; rm -f "$PWD/reminal"' EXIT
docker exec -d "$NAME" sh -c 'reminal relay 18080 >/tmp/relay.log 2>&1'
# Its listen line, not a guessed sleep (timeout: an abort cap only).
docker exec "$NAME" timeout 30 sh -c 'tail -n +1 -F /tmp/relay.log 2>/dev/null | { grep -q -m1 "reminal relay listening"; pkill -P $$ tail; }'
docker exec "$NAME" sh -c 'cd /root && SHELL=/bin/bash reminal new alpha >/dev/null 2>&1'
creds=$(docker exec "$NAME" sh -c 'reminal info alpha | grep -E "Session:|PIN:" | awk "{print \$2}"' | tr '\n' ' ')
echo "session: $creds"
# The page's script asks for the session's process to end and come back
# (RESTART), or to wait for its next save (SAVED), and waits for a line on
# its stdin: the browser cannot reach docker.
IO=$(mktemp -d)
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; docker rmi -f "$NAME" >/dev/null 2>&1 || true; rm -f "$PWD/reminal" "$IO/in" "$IO/out"; rmdir "$IO"' EXIT
mkfifo "$IO/in" "$IO/out"
docker run --rm -i -e BIG="${BIG:-}" --network "container:$NAME" -v "$PWD:/t" -w /tmp "$PW" sh -c \
  'npm i -s playwright@1.56.0 >/dev/null 2>&1 && cp /t/checks.mjs . && BIG=$BIG node checks.mjs "$@"' _ $creds <"$IO/in" >"$IO/out" 2>&1 &
pw=$!
exec 3>"$IO/in"
while IFS= read -r line; do
  if [ "$line" = SAVED ]; then
    # Its next save of its scrollback (every 15 s while it runs): what a
    # restart brings back.
    docker exec "$NAME" sh -c 'inotifywait -qq -e close_write,moved_to /root/.reminal/restore'
    echo go >&3
  elif [ "$line" = RESTART ]; then
    docker exec "$NAME" sh -c 'p=$(grep -o "\"pid\": [0-9]*" /root/.reminal/active-*.json | head -1 | grep -o "[0-9]*$"); kill -9 "$p"; tail --pid="$p" -f /dev/null; cd /root && reminal restore alpha >/dev/null 2>&1'
    echo go >&3
  else
    echo "$line"
  fi
done <"$IO/out"
exec 3>&-
# The checks' own verdict is the rig's (a bare wait is always 0).
wait "$pw"
