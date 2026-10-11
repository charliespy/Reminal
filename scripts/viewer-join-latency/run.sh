#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# How soon a viewer opening a long session can work in it (measure.mjs): a
# 10000-line coloured session behind a local relay, a Playwright viewer at
# THROTTLES (CPU slowdown), for each snapshot budget in BUDGETS
# (REMINAL_SCROLLBACK_BYTES) and each path (parts, and the plain one-frame
# snapshot). SCRIPT=typing.mjs instead types while the history is coming and
# checks the echoes and the end result (it takes no PLAIN; GAP=region sets a
# scroll region during the download first). SCRIPT=gap.mjs CASE=… changes the
# terminal during the download (gap.mjs lists the cases); SCRIPT=fan.mjs
# CASE=join|nojoin times a working viewer's echoes while another joins. Built and run in
# Docker only.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
NAME=${NAME:-reminal-join-latency}
PW=mcr.microsoft.com/playwright:v1.56.0-noble
BUDGETS=${BUDGETS:-"2097152 8388608"}
THROTTLES=${THROTTLES:-"4 6"}
NETS=${NETS:-"local"}
SCRIPT=${SCRIPT:-measure.mjs}
PLAINS=${PLAINS:-"0 1"}
if [ -n "${BIN:-}" ]; then cp "$BIN" reminal; else
  docker run --rm -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false -v "$ROOT":/src:ro -v "$PWD":/out -v reminal-join-gomod:/go/pkg/mod -w /src golang:1.26 go build -o /out/reminal ./cmd/reminal
fi
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; docker image rm "$NAME" >/dev/null 2>&1 || true; rm -f "$PWD/reminal"' EXIT
docker build -q -t "$NAME" . >/dev/null
for B in $BUDGETS; do
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker run -d --name "$NAME" "$NAME" >/dev/null
  docker exec -d "$NAME" sh -c 'reminal relay 18080 >/tmp/relay.log 2>&1'
  sleep 2
  docker exec -e REMINAL_SCROLLBACK_BYTES="$B" "$NAME" sh -c 'cd /root && SHELL=/work.sh reminal new long >/dev/null 2>&1'
  for _ in $(seq 240); do docker exec "$NAME" test -f /tmp/answer-done && break; sleep 0.5; done
  sleep 3
  read -r ID PIN < <(docker exec "$NAME" sh -c 'reminal info long | grep -E "Session:|PIN:" | awk "{print \$2}"' | tr '\n' ' '; echo)
  for N in $NETS; do for T in $THROTTLES; do for PLAIN in $PLAINS; do
    docker run --rm --network "container:$NAME" -e NET="$( [ "$N" = local ] || echo "$N")" -e THROTTLE="$T" -e PLAIN="$PLAIN" -e BUDGET="$((B >> 20)) MiB" -e SCRIPT="$SCRIPT" -e GAP="${GAP:-}" -e CASE="${CASE:-}" -v "$PWD:/t:ro" -v reminal-join-npm:/e2e/node_modules -w /e2e "$PW" sh -c \
      'cp "/t/$SCRIPT" /e2e/m.mjs; [ -d node_modules/playwright ] || npm i --silent --no-save playwright@1.56.0 >/dev/null; node m.mjs "$@"' _ "$ID" "$PIN" || FAILED=1
  done; done; done
done
exit "${FAILED:-0}"
