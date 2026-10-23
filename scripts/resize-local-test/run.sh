#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A terminal viewer attached on the same machine keeps the session at its
# size when another viewer leaves, end to end, in Docker. A local relay, the
# daemon and one session; a `reminal connect` here at 80x24 (over the
# session's local socket), and two through the relay (HOMEs of their own, so
# no local socket) at 200x50 and 40x20. The 40x20 one leaves: the agent
# forgets every viewer's size and asks the rest for theirs again. If the one
# here is not asked, the session grows to 200 columns, past its 80. The
# check is the session's own `stty size`.
#
#   scripts/resize-local-test/run.sh            # this tree
#   TREE=<tree> scripts/resize-local-test/run.sh
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
# viewer <name> <cols> <rows> [home]: a `reminal connect` in a pty of that size,
# fed from /tmp/<name>.in.
viewer() {
  mkfifo /tmp/$1.in
  ( sleep 600 > /tmp/$1.in ) &
  echo $! > /tmp/$1.hold
  HOME=${4:-/root} script -qfc "stty cols $2 rows $3; exec $R connect $sid $pin" /tmp/$1.log < /tmp/$1.in > /dev/null 2>&1 &
  echo $! > /tmp/$1.pid
  for i in $(seq 1 50); do grep -aq 'Connected to' /tmp/$1.log 2>/dev/null && break; sleep 0.2; done
  grep -aq 'Connected to' /tmp/$1.log || { echo "FAIL viewer $1 never connected"; exit 1; }
}
size() {
  rm -f /tmp/sz
  printf 'stty size > /tmp/sz\r' > /tmp/here.in
  for i in $(seq 1 25); do [ -s /tmp/sz ] && break; sleep 0.2; done
  cat /tmp/sz 2>/dev/null
}
viewer here 80 24
mkdir -p /tmp/h1 /tmp/h2
viewer big 200 50 /tmp/h1
viewer small 40 20 /tmp/h2
sleep 2
echo "with all three: $(size)"
pkill -f "stty cols 40 rows 20" || true
kill $(cat /tmp/small.pid) $(cat /tmp/small.hold) 2>/dev/null || true
sleep 3
after=$(size)
echo "after the 40x20 viewer left: $after"
case "$after" in
  "24 80") echo "PASS the session stayed at the size of the terminal attached here" ;;
  *) echo "FAIL the session is $after, not 24 80: it grew past the terminal attached here"; exit 1 ;;
esac
INSIDE
docker run --rm -v "$B":/b node:22-bookworm bash /b/inside.sh
