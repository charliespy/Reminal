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
#   7. reminal Copy and reminal Paste in the Explorer move a file and a folder
#      to and from a second machine (another container)
#   8. the cloud button lists shared ports and shares and stops one; this
#      part runs against the hosted relay, which is what carries ports, with
#      a page that serves only a marker
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
  docker exec "$NAME-ports" /home/coder/.local/bin/reminal stop 3000 >/dev/null 2>&1 || true
  docker rm -f "$NAME-peer" "$NAME-ports" "$NAME-cancel" "$NAME-open" "$NAME-rename" >/dev/null 2>&1 || true
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
  docker run --rm --network "container:${NET:-$NAME}" -e RIG_RUN="${RIG_RUN:-}" -e PASTE_CODE="${PASTE_CODE:-}" -e PASTE_CODE2="${PASTE_CODE2:-}" -e PORT_MARKER="${PORT_MARKER:-}" -e OPEN_ID="${OPEN_ID:-}" \
    -v "$PWD:/t" -w /tmp "$PW" sh -c \
    'npm i -s playwright@1.56.0 >/dev/null 2>&1 && cp /t/checks.mjs . && node checks.mjs "$@"' _ "$@"
}
fail=0
browser works share status views front | tee out/browser.log || true
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
# reminal Copy and Paste. The second machine is another container on the
# same relay, with its own files and home.
docker exec "$NAME" sh -c 'cd /home/coder/demo-project && head -c 3000 /dev/urandom | base64 > notes.txt \
  && mkdir -p assets/icons inbox && head -c 200000 /dev/urandom > assets/logo.bin \
  && printf "icon\n" > assets/icons/cart.svg && printf "# assets\n" > assets/README.md'
docker run -d --name "$NAME-peer" --network "container:$NAME" -e HOME=/tmp/peer \
  -e REMINAL_RELAY=ws://127.0.0.1:18080/ws -e REMINAL_WEB=http://127.0.0.1:18080 \
  -v "$PWD/reminal:/usr/local/bin/reminal:ro" node:22-bookworm-slim sleep infinity >/dev/null
peer() { docker exec "$NAME-peer" sh -c "$1"; }
tree_sum() { (cd "$1" && find . -type f -exec sha256sum {} + | sort) | sha256sum | cut -c1-16; }
browser transfer | tee out/transfer.log || true
grep -q '^FAIL' out/transfer.log && fail=1
grep -q '^DONE' out/transfer.log || { echo "FAIL  the transfer checks did not finish"; fail=1; }
fcode=$(grep -o '^TRANSFER-FILE [A-Z0-9-]*' out/transfer.log | cut -d' ' -f2)
dcode=$(grep -o '^TRANSFER-FOLDER [A-Z0-9-]*' out/transfer.log | cut -d' ' -f2)
peer "mkdir -p /tmp/in && cd /tmp/in && reminal paste $fcode >/dev/null 2>&1"
if [ "$(peer 'sha256sum < /tmp/in/notes.txt')" = "$(docker exec "$NAME" sh -c 'sha256sum < /home/coder/demo-project/notes.txt')" ]; then
  echo "PASS  a file copied from the Explorer pastes on another machine, byte for byte"
else echo "FAIL  the file copied from the Explorer did not arrive intact ($fcode)"; fail=1; fi
if peer "mkdir -p /tmp/again && cd /tmp/again && reminal paste $fcode >/dev/null 2>&1 && test -e /tmp/again/notes.txt"; then
  echo "FAIL  control: the same code pasted a second time"; fail=1
else echo "PASS  control: a copy code works once"; fi
peer "mkdir -p /tmp/in2 && cd /tmp/in2 && reminal paste $dcode >/dev/null 2>&1"
here=$(docker exec "$NAME" sh -c "$(declare -f tree_sum); tree_sum /home/coder/demo-project/assets")
there=$(peer "$(declare -f tree_sum); tree_sum /tmp/in2/assets")
if [ -n "$here" ] && [ "$here" = "$there" ]; then
  echo "PASS  a folder copied from the Explorer pastes on another machine, every file intact"
else echo "FAIL  the folder copied from the Explorer did not arrive intact ($dcode: $here vs $there)"; fail=1; fi
# The other way: a code made on the other machine, typed into reminal Paste.
M="from-the-peer-$N"
pcode=$(peer "mkdir -p /tmp/out && echo $M > /tmp/out/peer-note.txt && reminal copy /tmp/out/peer-note.txt" | grep -o 'Copy code: *[A-Z0-9-]*' | awk '{print $3}')
pcode2=$(peer "echo two > /tmp/out/peer-note2.txt && reminal copy /tmp/out/peer-note2.txt" | grep -o 'Copy code: *[A-Z0-9-]*' | awk '{print $3}')
docker exec "$NAME" chmod 555 '/home/coder/demo-project/[docs](command:workbench.action.showCommands)'
PASTE_CODE=$pcode PASTE_CODE2=$pcode2 browser paste | tee out/paste.log || true
grep -q '^FAIL' out/paste.log && fail=1
grep -q '^DONE' out/paste.log || { echo "FAIL  the paste checks did not finish"; fail=1; }
if [ "$(docker exec "$NAME" cat /home/coder/demo-project/inbox/peer-note.txt 2>/dev/null)" = "$M" ]; then
  echo "PASS  reminal Paste put the other machine's file in the folder"
else echo "FAIL  reminal Paste did not put the other machine's file in the folder ($pcode)"; fail=1; fi

# Shared ports, against the hosted relay (the local one doesn't carry them),
# in a container of their own. The page serves only a marker.
docker run -d --name "$NAME-ports" -e REMINAL_RELAY= -e REMINAL_WEB= "$NAME" >/dev/null
for _ in $(seq 30); do docker exec "$NAME-ports" curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break; sleep 1; done
PM="reminal-rig-port-$N"
docker exec -d "$NAME-ports" /usr/lib/code-server/lib/node -e "require('http').createServer((q,r)=>r.end('$PM')).listen(3000)"
NET="$NAME-ports" PORT_MARKER=$PM browser ports | tee out/ports.log || true
grep -q '^FAIL' out/ports.log && fail=1
grep -q '^DONE' out/ports.log || { echo "FAIL  the ports checks did not finish"; fail=1; }
docker rm -f "$NAME-peer" "$NAME-ports" >/dev/null 2>&1 || true

# Cancelling a paste: its relay is a listener that never answers, so the
# paste waits until it is cancelled; nothing of it may be left running.
docker run -d --name "$NAME-cancel" -e REMINAL_RELAY=ws://127.0.0.1:9999/ws -e REMINAL_WEB=http://127.0.0.1:9999 "$NAME" >/dev/null
for _ in $(seq 30); do docker exec "$NAME-cancel" curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break; sleep 1; done
docker exec -d "$NAME-cancel" /usr/lib/code-server/lib/node -e "require('net').createServer(() => {}).listen(9999)"
NET="$NAME-cancel" browser cancel | tee out/cancel.log || true
grep -q '^FAIL' out/cancel.log && fail=1
grep -q '^DONE' out/cancel.log || { echo "FAIL  the cancel checks did not finish"; fail=1; }
sleep 2
if docker exec "$NAME-cancel" pgrep -f "reminal paste" >/dev/null 2>&1; then
  echo "FAIL  a cancelled paste is still running"; fail=1
else echo "PASS  a cancelled paste leaves nothing running"; fi
docker rm -f "$NAME-cancel" >/dev/null 2>&1 || true

# A reminal started outside the editor, in a container of its own so the
# lists above stay as they are: the cloud list opens it, and its tab follows
# its name.
docker run -d --name "$NAME-open" "$NAME" >/dev/null
for _ in $(seq 30); do docker exec "$NAME-open" curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break; sleep 1; done
docker exec -d "$NAME-open" sh -c '~/.local/bin/reminal relay 18080 >/tmp/relay.log 2>&1'
sleep 2
docker exec -d "$NAME-open" sh -c 'cd /home/coder/demo-project/src && sleep infinity | script -qfc ~/.local/bin/reminal /dev/null'
OPEN_ID=
for _ in $(seq 30); do
  OPEN_ID=$(docker exec "$NAME-open" sh -c '~/.local/bin/reminal info --all --json' 2>/dev/null | grep -o '"id":"[A-Z0-9]*"' | head -1 | cut -d'"' -f4) || true
  [ -n "$OPEN_ID" ] && break; sleep 1
done
docker exec "$NAME-open" sh -c "~/.local/bin/reminal rename $OPEN_ID build-server" >/dev/null
NET="$NAME-open" OPEN_ID=$OPEN_ID browser existing | tee out/existing.log || true
grep -q '^FAIL' out/existing.log && fail=1
grep -q '^DONE' out/existing.log || { echo "FAIL  the existing-session checks did not finish"; fail=1; }
if [ "$(docker exec "$NAME-open" cat /tmp/open-pwd 2>/dev/null)" = /home/coder/demo-project/src ]; then
  echo "PASS  what was typed in the opened tab ran in that session, in its own folder"
else echo "FAIL  what was typed in the opened tab did not run in that session"; fail=1; fi
if grep -q '^PASS  opening it gives a tab' out/existing.log \
  && docker exec "$NAME-open" sh -c '~/.local/bin/reminal info --all --json' | grep -q "\"id\":\"$OPEN_ID\""; then
  echo "PASS  closing its tab left the session running"
else echo "FAIL  closing its tab ended the session"; fail=1; fi
docker rm -f "$NAME-open" >/dev/null 2>&1 || true

# A terminal renamed by hand in the editor: its session takes the name,
# here and wherever else it is seen. In a container of its own, which starts
# on the published 0.2.1 (no such sync) and is updated to this build with its
# terminals still running, as an update reaches someone. Programs may set a
# terminal's title, and tabs here show titles that way, so one does; that
# must not be taken for a rename.
docker run -d --name "$NAME-rename" "$NAME" >/dev/null
for _ in $(seq 30); do docker exec "$NAME-rename" curl -fs http://127.0.0.1:8443/healthz >/dev/null 2>&1 && break; sleep 1; done
docker exec -d "$NAME-rename" sh -c '~/.local/bin/reminal relay 18080 >/tmp/relay.log 2>&1'
docker exec "$NAME-rename" sh -c 'curl -fsSL -o /tmp/old.vsix https://open-vsx.org/api/reminal/reminal/0.2.1/file/reminal.reminal-0.2.1.vsix \
  && code-server --install-extension /tmp/old.vsix --force >/dev/null 2>&1'
docker exec "$NAME-rename" /usr/lib/code-server/lib/node -e '
  const f = "/home/coder/.local/share/code-server/User/settings.json", fs = require("fs");
  const s = JSON.parse(fs.readFileSync(f, "utf8")); s["terminal.integrated.tabs.title"] = "${sequence}";
  fs.writeFileSync(f, JSON.stringify(s, null, 2));'
# Each session's name as reminal gives it ("-" for none), one per line, sorted.
names() {
  docker exec "$NAME-rename" sh -c '~/.local/bin/reminal info --all --json | /usr/lib/code-server/lib/node -e "
    let s = \"\"; process.stdin.on(\"data\", (d) => (s += d)).on(\"end\", () => {
      for (const x of JSON.parse(s)) console.log(x.name || \"-\"); });"' | sort | tr '\n' ' '
}
RIG_RUN=$N
until_file() { for _ in $(seq 180); do [ -e "out/$1-$RIG_RUN" ] && return 0; sleep 1; done; return 1; }
NET="$NAME-rename" RIG_RUN=$RIG_RUN browser renametab > out/rename.log 2>&1 &
bpid=$!
if until_file rename-old; then
  got=$(names)
  if [ "$got" = "- " ]; then echo "PASS  control: before this update, renaming a tab left its session unnamed"
  else echo "FAIL  control: before this update the session was named: $got"; fail=1; fi
  docker exec "$NAME-rename" code-server --install-extension /tmp/reminal.vsix --force >/dev/null 2>&1
fi
touch "out/rename-new-$RIG_RUN"
if until_file rename-reloaded; then
  got=$(names)
  if [ "$got" = "legacy-tab " ]; then echo "PASS  after the update, a tab renamed before it gives its session that name"
  else echo "FAIL  after the update, the tab renamed before it did not name its session: $got"; fail=1; fi
fi
touch "out/rename-go-$RIG_RUN"
wait $bpid || true
cat out/rename.log
grep -q '^FAIL' out/rename.log && fail=1
grep -q '^DONE' out/rename.log || { echo "FAIL  the rename checks did not finish"; fail=1; }
got=$(names)
if [ "$got" = "- C3PRICING-501 legacy-tab " ]; then
  echo "PASS  a tab renamed by hand names its session; one never renamed, whose program set its title, stays unnamed, also after a reload"
else echo "FAIL  sessions after renaming a tab: $got"; fail=1; fi
docker rm -f "$NAME-rename" >/dev/null 2>&1 || true
rm -f out/rename-*-"$RIG_RUN"

# A window reload, in a browser of its own (it closes the terminals above).
browser revive | tee out/revive.log || true
grep -q '^FAIL' out/revive.log && fail=1
echo "session records after the reload checks:"; docker exec "$NAME" sh -c 'for f in ~/.reminal/active-*.json; do echo "$(date -u -r "$f" +%Y-%m-%dT%H:%M:%S.%NZ) $(basename "$f")"; done' || true
echo "sessions after the reload checks:"; docker exec "$NAME" sh -c '~/.local/bin/reminal info --all --json | tr "{" "\n" | grep -o "\"id\":\"[A-Z0-9]*\".*\"pid\":[0-9]*" | sed "s/\"pin\":\"[0-9]*\",//; s/\"open_url[^,]*,\"join_url[^,]*,//"' || true
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
