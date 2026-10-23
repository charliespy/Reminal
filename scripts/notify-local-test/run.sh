#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A `reminal notify` reaches a terminal viewer attached on the same machine,
# end to end, in Docker: a local relay, the daemon, one session, a real
# `reminal connect` viewer in a pty of its own, then a notify. The viewer
# attaches over the session's local socket, not through the relay, so the
# check is the viewer's OWN notify line ("notify: <text>"), never the dim
# "notified viewers: <text>" line the agent also writes into the session's
# output, which reaches the viewer as terminal output either way.
#
#   scripts/notify-local-test/run.sh            # this tree
#   TREE=<tree> scripts/notify-local-test/run.sh
# A heavy run: waits for one of the machine's slots first (scripts/README-gates.md).
[ -n "${REMINAL_GATE_HELD:-}" ] || ! command -v python3 >/dev/null || [ ! -f "$(dirname -- "$0")/../gatelock.py" ] || exec python3 "$(dirname -- "$0")/../gatelock.py" -- sh "$0" "$@"
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TREE=${TREE:-$(CDPATH= cd -- "$DIR/../.." && pwd)}
B=$(mktemp -d)
trap '"$DIR/../rm-tmp.sh" "$B"' EXIT
docker run --rm -v "$TREE":/src:ro -v "$B":/out -v reminal-gomod:/go/pkg/mod -v reminal-gocache:/root/.cache/go-build \
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm go build -o /out/reminal ./cmd/reminal
cat > "$B/inside.sh" <<'INSIDE'
set -u
export HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:8080/ws REMINAL_WEB=http://127.0.0.1:8080 TERM=xterm-256color
R=/b/reminal
$R relay 8080 >/tmp/relay.log 2>&1 &
for i in $(seq 1 50); do curl -sf http://127.0.0.1:8080/ >/dev/null && break; sleep 0.2; done
$R daemon >/tmp/daemon.log 2>&1 &
for i in $(seq 1 50); do $R list >/dev/null 2>&1 && break; sleep 0.2; done
cd /root
sid=$($R new t 2>&1 | grep -o 'Session: *[A-Z0-9]*' | awk '{print $2}')
pin=$($R info "$sid" --json | sed -n 's/.*"pin": *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$sid" ] && [ -n "$pin" ] || { echo "FAIL no session or PIN"; exit 1; }
mkfifo /tmp/vin
( sleep 600 > /tmp/vin ) &
script -qfc "$R connect $sid $pin" /tmp/v.log < /tmp/vin > /dev/null 2>&1 &
for i in $(seq 1 50); do grep -aq 'Connected to' /tmp/v.log 2>/dev/null && break; sleep 0.2; done
sleep 1
REMINAL_SESSION=$sid $R notify 'notify-local-probe'
for i in $(seq 1 25); do grep -aq 'notify: notify-local-probe' /tmp/v.log && break; sleep 0.2; done
own=$(grep -ac 'notify: notify-local-probe' /tmp/v.log || true)
echo "the viewer's own notify line: $own"
if [ "$own" -ge 1 ]; then echo "PASS a notify reached a viewer attached here"; else echo "FAIL a notify did not reach a viewer attached here"; exit 1; fi
INSIDE
docker run --rm -v "$B":/b node:22-bookworm bash /b/inside.sh
