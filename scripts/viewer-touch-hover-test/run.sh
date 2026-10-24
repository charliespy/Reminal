#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Buttons on a touch screen after a tap (check.mjs): a tap leaves :hover on
# the tapped control, so a hover look written for a mouse must never reach a
# touch screen, and a hover rule must never reset another rule's icon. A
# relay and one session in a Playwright container; the viewer is walked at
# phone width with real taps, every control is checked with its hover state
# forced, and the desktop look at 1280 with a mouse hover is screenshotted.
# Needs docker; nothing here touches the host's reminal.
#
#   scripts/viewer-touch-hover-test/run.sh [SHOTS_DIR]
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
B=$(mktemp -d)
trap '"$ROOT/scripts/rm-tmp.sh" "$B"' EXIT
docker run --rm -v "$ROOT":/src:ro -v "$B":/out -v reminal-gomod:/go/pkg/mod -v reminal-gocache:/root/.cache/go-build \
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm go build -o /out/reminal ./cmd/reminal
cp check.mjs "$B/"
OUT=()
[ -n "${1:-}" ] && OUT=(-v "$1":/shots)
cat > "$B/rig.sh" <<'RIG'
set -eu
export PATH=/p:$PATH HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:8080/ws REMINAL_WEB=http://127.0.0.1:8080
reminal relay 8080 >/p/relay.log 2>&1 &
for i in $(seq 100); do (exec 3<>/dev/tcp/127.0.0.1/8080) 2>/dev/null && break; sleep 0.1; done
reminal daemon >/p/daemon.log 2>&1 &
for i in $(seq 100); do reminal list >/dev/null 2>&1 && break; sleep 0.1; done
out=$(reminal new touch 2>&1)
id=$(printf '%s\n' "$out" | sed -n 's/.*Session:[[:space:]]*\([A-Z0-9]\{4,\}\).*/\1/p' | head -1)
pin=$(printf '%s\n' "$out" | sed -n 's/.*PIN:[[:space:]]*\([0-9]\{4,\}\).*/\1/p' | head -1)
echo "$id $pin" > /p/join.txt
mkdir -p /e2e && cd /e2e && cp /p/check.mjs t.mjs && npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1 && node t.mjs
RIG
docker run --rm -v "$B":/p ${OUT[@]+"${OUT[@]}"} -w /p mcr.microsoft.com/playwright:v1.56.0-noble bash /p/rig.sh
