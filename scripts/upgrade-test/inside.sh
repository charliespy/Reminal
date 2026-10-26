#!/bin/sh
# Runs in the container (see run.sh). /t holds old/reminal (the installed
# version) and the newer release as a tar.gz. Starts the old one in a
# terminal, says yes to its upgrade offer, and reports what happened.
set -u
mkdir -p /root/.local/bin /root/.reminal /srv
cp /t/old/reminal /root/.local/bin/reminal
cp /t/reminal_9.9.9_linux_$ARCH.tar.gz /srv/
cp /t/new/reminal /usr/local/bin/relay-reminal
SUM=$(sha256sum /srv/reminal_9.9.9_linux_$ARCH.tar.gz | cut -d' ' -f1)
# The newest release, as the update check would have recorded it.
printf '{"checked_at":"%s","latest_tag":"v9.9.9","asset_url":"http://127.0.0.1:8000/reminal_9.9.9_linux_%s.tar.gz","sha256":"%s","channel":"stable"}\n' \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$ARCH" "$SUM" > /root/.reminal/version-check.json
(cd /srv && python3 -m http.server 8000 >/tmp/http.log 2>&1 &)
relay-reminal relay 18080 >/tmp/relay.log 2>&1 &
sleep 2
export REMINAL_RELAY=ws://127.0.0.1:18080/ws REMINAL_WEB=http://127.0.0.1:18080 SHELL=/bin/sh
# A terminal running reminal itself, as an editor's reminal tab does. The
# input: "y" to the offer, then nothing; the terminal stays open while
# whatever runs in it does.
(sleep 3; printf 'y\n'; sleep 40) | script -qfec /root/.local/bin/reminal /tmp/term.log >/dev/null 2>&1 &
TERM_PID=$!
sleep 20
if kill -0 $TERM_PID 2>/dev/null; then echo "TERMINAL open"; else echo "TERMINAL closed"; fi
echo "INSTALLED $(/root/.local/bin/reminal version 2>/dev/null | head -1)"
echo "SESSIONS $(relay-reminal info --all --json 2>/dev/null | grep -o '"id":"[A-Z0-9]*"' | wc -l)"
# The session's process is the one the terminal started, now running the new
# version: the binary it runs is the installed one, which is 9.9.9.
PID=$(relay-reminal info --all --json 2>/dev/null | grep -o '"pid":[0-9]*' | head -1 | cut -d: -f2)
[ -n "$PID" ] && echo "SESSION_EXE $(readlink /proc/$PID/exe) $(tr '\0' ' ' < /proc/$PID/cmdline)"
echo "OFFERS $(grep -o 'Upgrade now?' /tmp/term.log | wc -l)"
tr -d '\r' < /tmp/term.log | grep -E 'Upgraded to|Starting it|Restart reminal|Run reminal again' | sed 's/^/SAID /'
