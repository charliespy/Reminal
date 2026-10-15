#!/usr/bin/env bash
# End-to-end check of the VS Code extension (extensions/vscode) in a real
# code-server, against a real reminal and a local relay, all in a container:
#
#   1. the "reminal" terminal profile opens a reminal session, its link
#      notification copies a link, and the web viewer reaches that session
#   2. the terminal panel's cloud button opens one in the active terminal's
#      folder
#   3. "Open reminal in This Folder" on a terminal tab does the same, and
#      closes the old terminal when asked to
#   4. the status bar follows an agent through Working, Needs you and Done
#   5. with reminal gone, every entry point says how to install it instead of
#      opening a broken terminal
#   6. "Install reminal" runs the install command in a terminal, and offers a
#      reminal terminal once the installer has finished
#
# Screenshots land in scripts/vscode-test/out/. KEEP=1 leaves the container up.
# Needs docker; nothing here touches the host's reminal.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
NAME=reminal-vscode-test
PW=mcr.microsoft.com/playwright:v1.56.0-noble
mkdir -p out

GOOS=linux GOARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') \
  go build -C "$ROOT" -o "$PWD/reminal" ./cmd/reminal
(cd "$ROOT/extensions/vscode" && npm ci --silent && npm run -s prepackage \
  && npx vsce package --no-dependencies -o "$OLDPWD/reminal.vsix" >/dev/null)
cleanup() {
  [ "${KEEP:-}" = 1 ] || docker rm -f "$NAME" >/dev/null 2>&1 || true
  rm -f "$PWD/reminal" "$PWD/reminal.vsix"
}
trap cleanup EXIT

# Unit tests, in a container like everything else.
# The whole repo, read-only: the share test reads the web viewer's app.js.
docker run --rm -v "$ROOT:/x:ro" -w /x/extensions/vscode node:22-bookworm-slim \
  sh -c "node --test out/test/*.test.js"

# CODE_SERVER=4.93.1 runs it on VS Code 1.93, the engine floor.
docker build -q --build-arg CODE_SERVER="${CODE_SERVER:-4.96.4}" -t "$NAME" . >/dev/null
docker rm -f "$NAME" >/dev/null 2>&1 || true
docker run -d --name "$NAME" "$NAME" >/dev/null
docker exec -d "$NAME" sh -c '~/.local/bin/reminal relay 18080 >/tmp/relay.log 2>&1'
for _ in $(seq 30); do docker exec "$NAME" curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break; sleep 1; done

browser() {
  docker run --rm --network "container:$NAME" -v "$PWD:/t" -w /tmp "$PW" sh -c \
    'npm i -s playwright@1.56.0 >/dev/null 2>&1 && cp /t/checks.mjs . && node checks.mjs "$@"' _ "$@"
}
fail=0
browser works share status views | tee out/browser.log || true
grep -q '^FAIL' out/browser.log && fail=1
# A browser run that died part way prints no FAIL; it doesn't print DONE either.
grep -q '^DONE' out/browser.log || { echo "FAIL  the browser checks did not finish"; fail=1; }

# What "Share for a Person" copied, pasted into another terminal: it must
# reach the session, and the same line with a wrong PIN must not (the control).
# The marker is computed in the remote shell ($((N+1)) is typed, N+1 comes
# back), with N fresh each run, so only a command that really ran there,
# this time, can produce it; colour and title escapes are stripped first.
N=$(( $(date +%s) % 100000 ))
connect_says() {
  docker exec "$NAME" sh -c "export PATH=\$HOME/.local/bin:\$PATH; (sleep 5; printf 'true\n'; sleep 1; printf 'echo via-connect-\$(($N+1))\n'; sleep 3; printf '\035') | timeout 25 script -qec '$1' /dev/null" 2>&1 \
    | tr -d '\r' | sed -e 's/\x1b\][^\x07]*\x07//g' -e 's/\x1b\[[0-9;?]*[a-zA-Z]//g'
}
line=$(grep -o 'SHARE-CONNECT reminal connect [A-Z0-9]* [0-9]*' out/browser.log | tail -1 | cut -d' ' -f2-)
if [ -n "$line" ] && connect_says "$line" | grep -qx "via-connect-$((N+1))"; then
  echo "PASS  pasted \"$line\" connects and runs in that session"
else echo "FAIL  pasted \"$line\" did not reach the session"; fail=1; fi
bad="${line% *} 000000"; [ "$bad" = "$line" ] && bad="${line% *} 111111"
if connect_says "$bad" | grep -qx "via-connect-$((N+1))"; then
  echo "FAIL  control: a wrong PIN reached the session too"; fail=1
else echo "PASS  control: the same line with a wrong PIN does not connect"; fi
echo "sessions in the container:"
docker exec "$NAME" /home/coder/.local/bin/reminal list || true
# A window reload, in a browser of its own (it closes the terminals above).
browser revive | tee out/revive.log || true
grep -q '^FAIL' out/revive.log && fail=1
grep -q '^DONE' out/revive.log || { echo "FAIL  the reload checks did not finish"; fail=1; }
# Break it: reminal is no longer anywhere the extension looks.
docker exec "$NAME" mv /home/coder/.local/bin/reminal /tmp/reminal-hidden
browser missing || fail=1
# Install from the not-installed message, with a stand-in installer (no
# download): its first run fails; after that it puts that reminal back, then
# asks one question.
docker cp fake-curl "$NAME:/tmp/fake-curl"
docker exec -u root "$NAME" install -m 755 /tmp/fake-curl /usr/local/bin/curl
docker exec "$NAME" touch /tmp/install-fails
browser install || fail=1
if docker exec "$NAME" grep -q '^/dev/pts/' /tmp/fake-install.tty 2>/dev/null; then
  echo "PASS  the install command ran in a terminal: $(docker exec "$NAME" cat /tmp/fake-install.tty)"
else echo "FAIL  the install command did not run in a terminal"; fail=1; fi
# VS Code itself keeps a machine-scoped setting out of a workspace, so this
# guards the setting's scope in package.json; that the code reads only the
# user's value is beside it, in configuredPath.
if docker exec "$NAME" test -e /tmp/ws-reminal-ran; then
  echo "FAIL  a workspace's reminal.path was run"; fail=1
else echo "PASS  a workspace's reminal.path is never run"; fi
exit $fail
