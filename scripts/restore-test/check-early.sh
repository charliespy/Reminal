#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A box whose power is cut seconds after an agent started in a session —
# before the restore record's every-15 s save could see it: the session must
# still come back with its agent, on the same conversation, not as a bare
# shell (the record is saved when the program in front changes, not only on
# its timer). Against the rig run.sh starts: `scripts/restore-test/run.sh early`.
BOX=${BOX:-reminal-restore-box}
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
box()  { docker exec $BOX sh -c "$1"; }
send() { box "mcp.sh send $1 '$2' >/dev/null"; }
screen() { box "mcp.sh read $1"; }
idof()  { echo "$1" | sed -n 's/^ *Session: *\([A-Z0-9]*\).*/\1/p' | head -1; }

t0=$(date +%s)
out=$(box "mkdir -p /root/early && cd /root/early && reminal new early 2>&1"); ID=$(idof "$out")
[ -n "$ID" ] && ok "session $ID" || { fail "no session: $out"; exit 1; }
send $ID 'claude'
for i in $(seq 1 20); do screen $ID | grep -q "fake-claude: new conversation" && break; sleep 0.2; done
CONV=$(box "ls -t /root/.fake-claude/*.conv 2>/dev/null | head -1 | xargs basename 2>/dev/null | sed 's/.conv$//'")
[ -n "$CONV" ] && ok "claude started, conversation $CONV" || fail "claude never started"
sleep 1 # what the session sees of it: the program in front, its first hook
el=$(( $(date +%s) - t0 ))
[ "$el" -lt 12 ] && ok "power cut ${el}s after the session started, before its first timed save" || fail "too slow to be before the 15 s save (${el}s): the test proves nothing"
docker restart $BOX >/dev/null
for i in $(seq 1 30); do box "grep -q -- '--resume' /root/.fake-claude/argv.log" 2>/dev/null && break; sleep 1; done
sleep 2
box "reminal list" | grep -q "$ID" && ok "$ID came back at boot" || fail "$ID not back"
s=$(screen $ID)
echo "$s" | grep -q "fake-claude: resumed conversation $CONV" && ok "its agent came back, on the same conversation" || fail "came back without its agent: $(echo "$s" | tail -4 | tr '\n' ' ')"
argv=$(box "tail -1 /root/.fake-claude/argv.log")
echo "$argv" | grep -q -- "--resume $CONV" && ok "started as: claude $(echo "$argv" | cut -c7-)" || fail "last start: $argv"
box "reminal kill $ID -y >/dev/null 2>&1"
[ $fails -eq 0 ] && echo PASS || { echo "$fails failure(s)"; exit 1; }
