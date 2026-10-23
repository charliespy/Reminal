#!/usr/bin/env bash
# A device with an owner key opening sessions by their PIN (owner-pin.js):
# on a machine it does not own, the PIN handshake within a moment; on one it
# owns, the owner handshake; on one that stopped owning it, the PIN within
# a moment. Three machines (a HOME and owner list each) and a relay in one
# container, the browser in it too. Needs docker; nothing here touches the
# host's reminal.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
B=$(mktemp -d)
trap '"$ROOT/scripts/rm-tmp.sh" "$B"' EXIT
docker run --rm -v "$ROOT":/src:ro -v "$B":/out -v reminal-gomod:/go/pkg/mod -v reminal-gocache:/root/.cache/go-build \
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm go build -o /out/reminal ./cmd/reminal
cp owner-pin.js "$B/"
cat > "$B/rig.sh" <<'RIG'
set -eu
export PATH=/p:$PATH
printf '#!/bin/sh\necho PIPE-OK-$$\nexec bash -i\n' > /p/sh.sh; chmod +x /p/sh.sh
reminal relay 8080 >/p/relay.log 2>&1 &
for i in $(seq 100); do (exec 3<>/dev/tcp/127.0.0.1/8080) 2>/dev/null && break; sleep 0.1; done
: > /p/joins.txt
for m in m1 m2 m3; do
  mkdir -p /$m/etc
  env HOME=/$m REMINAL_OWNERS_DIR=/$m/etc SHELL=/p/sh.sh REMINAL_RELAY=ws://127.0.0.1:8080/ws REMINAL_WEB=http://127.0.0.1:8080 reminal daemon >/p/daemon-$m.log 2>&1 &
  for i in $(seq 100); do HOME=/$m reminal list >/dev/null 2>&1 && break; sleep 0.1; done
  out=$(env HOME=/$m REMINAL_OWNERS_DIR=/$m/etc SHELL=/p/sh.sh REMINAL_RELAY=ws://127.0.0.1:8080/ws REMINAL_WEB=http://127.0.0.1:8080 reminal new $m 2>&1)
  id=$(printf '%s\n' "$out" | sed -n 's/.*Session:[[:space:]]*\([A-Z0-9]\{4,\}\).*/\1/p' | head -1)
  pin=$(printf '%s\n' "$out" | sed -n 's/.*PIN:[[:space:]]*\([0-9]\{4,\}\).*/\1/p' | head -1)
  echo "$m $id $pin" >> /p/joins.txt
done
# The test's asks: enrol its device on m1 and m3; later, m3 stops owning it.
( while [ ! -s /p/owner-id ]; do sleep 0.1; done
  for m in m1 m3; do HOME=/$m REMINAL_OWNERS_DIR=/$m/etc reminal add owner "$(cat /p/owner-id)" -y >>/p/enrol.log 2>&1; done
  touch /p/owner-ok
  while [ ! -e /p/revoke-m3 ]; do sleep 0.1; done
  rm -f /m3/etc/owners.json; touch /p/revoked ) &
mkdir -p /e2e && cd /e2e && cp /p/owner-pin.js . && { [ -d node_modules/playwright ] || npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1; } && node owner-pin.js /p/joins.txt
RIG
docker run --rm -e DBG="${DBG:-}" -v "$B":/p -w /p mcr.microsoft.com/playwright:v1.56.0-noble bash /p/rig.sh
