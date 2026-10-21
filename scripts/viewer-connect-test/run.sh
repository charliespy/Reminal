#!/usr/bin/env bash
# Drives the real viewer, served by a real relay, against two real sessions in
# a container — what made a connect hang, hand out a PIN that cannot work,
# or say the wrong thing:
#
#   1. one PIN handshake per connect, not two (two spent the agent's PIN allowance
#      twice per connect, and it refills one per 10s)
#   2. a wrong PIN keeps saying "PIN mismatch" instead of being overwritten by
#      its own watchdog with "Handshake timed out" nine seconds later
#   3. switching sessions does not carry the previous session's PIN, into the
#      handshake or into what Share hands out
#   4. keys typed while a PIN-free switch reads the owner key run in the
#      session being joined, and only there
#   5. two sessions on one machine are known to be on one machine
#   6. with the session already filled in, the status asks only for the PIN
#   9. the relay's word on a session it has not stays over the terminal, and
#      is never written into it
#  10. a tap in the session dock on the session being opened (its socket
#      open, its handshake in flight, or its session key still being
#      exchanged) dials nothing; with its agent away and its key in, it does
#
# Needs docker; the browser runs in a Playwright container. Nothing here
# touches the host's reminal.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
PORT=${PORT:-18080}
# Its container and image, by name: one of this run's own (VIEWER_TEST_NAME to
# choose it), so two runs at once on one machine never remove each other's.
NAME=${VIEWER_TEST_NAME:-reminal-viewer-test-$$}
PW=mcr.microsoft.com/playwright:v1.56.0-noble

GOOS=linux GOARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') \
  go build -C "$ROOT" -o "$PWD/reminal" ./cmd/reminal
docker build -q -t "$NAME" . >/dev/null
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" -p "$PORT:18080" "$NAME" >/dev/null
trap 'docker rm -f "$NAME" >/dev/null 2>&1 || true; docker rmi "$NAME" >/dev/null 2>&1 || true; rm -f "$PWD/reminal"' EXIT
docker exec -d "$NAME" sh -c 'reminal relay 18080 >/tmp/relay.log 2>&1'
sleep 3
docker exec "$NAME" sh -c 'cd /root && reminal new alpha >/dev/null 2>&1 && reminal new beta >/dev/null 2>&1'
creds=$(docker exec "$NAME" sh -c 'for s in alpha beta; do reminal info $s | grep -E "Session:|PIN:" | awk "{print \$2}"; done' | tr '\n' ' ')
echo "sessions: $creds"
# An Ed25519 key enrolled as an owner of the container; checks.mjs installs
# it as the browser's device key so the PIN-free path is a real one.
read -r pk raw id < <(docker run --rm "$PW" node -e '
  const c = require("crypto"); const k = c.generateKeyPairSync("ed25519");
  const raw = k.publicKey.export({ format: "der", type: "spki" }).subarray(12);
  console.log(k.privateKey.export({ format: "der", type: "pkcs8" }).toString("base64"),
    raw.toString("base64"), "rmnl_" + raw.toString("base64url"))')
docker exec "$NAME" reminal add owner "$id" -y >/dev/null
docker run --rm --network "container:$NAME" -v "$PWD:/t" -w /tmp "$PW" sh -c \
  'npm i -s playwright@1.56.0 >/dev/null 2>&1 && cp /t/checks.mjs . && node checks.mjs "$@"' _ $creds "$pk" "$raw"
A=$(echo $creds | awk '{print $1}'); B=$(echo $creds | awk '{print $3}')
fail=0
for how in a b; do
  hits=$(docker exec "$NAME" sh -c "cat /tmp/fk-$how 2>/dev/null || true" | tr '\n' ' ')
  if [ "$(echo $hits | tr a-z A-Z)" = "$(echo $B | tr a-z A-Z)" ]; then v=PASS; else v=FAIL; fail=1; fi
  echo "TEST 4$how  the keys ran in [${hits% }] (A=$A, B=$B)   $v"
done
exit $fail
