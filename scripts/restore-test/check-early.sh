#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A box whose power is cut while an agent in a session has been sent
# nothing — no turn, so no conversation written (an agent writes one on its
# first message): the session must come back with its agent, started afresh
# with its flags, never a bare shell, and never told to resume a
# conversation that is not there (Claude Code: "No conversation found", and
# the session was left a shell, where whatever was typed next ran as a
# command). Two ways:
#   early  cut seconds after the agent started, before the restore record's
#          every-15 s save could see it (it is saved when the program in
#          front changes, not only on its timer);
#   late   cut once a timed save has seen it, as a machine that went down
#          a while after its agent started, before its first turn.
# Against the rig run.sh starts: `scripts/restore-test/run.sh early`.
BOX=${BOX:-reminal-restore-box}
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
box()  { docker exec $BOX sh -c "$1"; }
send() { box "mcp.sh send $1 '$2' >/dev/null"; }
screen() { box "mcp.sh read $1"; }
idof()  { echo "$1" | sed -n 's/^ *Session: *\([A-Z0-9]*\).*/\1/p' | head -1; }
after() { screen $1 | awk '{gsub(/\\n/, "\n"); print}' | sed -n '/restored this session/,$p'; }

out=$(box "mkdir -p /root/late && cd /root/late && reminal new late 2>&1"); L=$(idof "$out")
[ -n "$L" ] && ok "session late: $L" || { fail "no session: $out"; exit 1; }
send $L 'claude --model opus-late'
sleep 20 # past the first timed save: the record has seen it, its id and all
t0=$(date +%s)
out=$(box "mkdir -p /root/early && cd /root/early && reminal new early 2>&1"); E=$(idof "$out")
[ -n "$E" ] && ok "session early: $E" || { fail "no session: $out"; exit 1; }
send $E 'claude --model opus-early'
for i in $(seq 1 20); do screen $E | grep -q "fake-claude: new conversation" && break; sleep 0.2; done
screen $E | grep -q "fake-claude: new conversation" && ok "both claudes started, neither sent anything" || fail "claude never started in $E"
sleep 1 # what the session sees of it: the program in front, its first hook
el=$(( $(date +%s) - t0 ))
[ "$el" -lt 12 ] && ok "power cut ${el}s after early's session started, before its first timed save" || fail "too slow to be before the 15 s save (${el}s): the early case proves nothing"
box "ls /root/.fake-claude/*.conv 2>/dev/null | wc -l" | grep -qx 0 && ok "no conversation written yet (none sent anything)" || fail "a conversation was written though nothing was sent: the rig does not stand for claude"
docker restart $BOX >/dev/null
for c in "late $L opus-late" "early $E opus-early"; do
  set -- $c
  # Each its own wait for its verdict: two sessions restored at once, the
  # second's screen is read a while after the first's.
  s=""
  for i in $(seq 1 60); do s=$(after $2); echo "$s" | grep -q "fake-claude: new conversation\|No conversation found" && break; sleep 1; done
  box "reminal list" | grep -q "$2" && ok "$1: $2 came back at boot" || fail "$1: $2 not back"
  if echo "$s" | grep -q "No conversation found"; then
    fail "$1: told to resume a conversation never written: $(echo "$s" | grep -m1 'No conversation found')"
  elif echo "$s" | grep -q "fake-claude: new conversation"; then
    ok "$1: its agent came back, started afresh"
  else
    fail "$1: came back without its agent: $(echo "$s" | tail -4 | tr '\n' ' ') — its screen's end: $(screen $2 | tail -c 400 | tr '\n' ' ')"
    echo "  debug $2: $(box "cat /root/.reminal/active-$2.json 2>/dev/null | tr -d '\n' | cut -c1-300")"
    echo "  debug socks: $(box 'ls /root/.reminal/*.sock 2>/dev/null | tr "\n" " "')"
    echo "  debug ps: $(box 'ps -eo pid,ppid,args | grep -E "reminal|claude" | grep -v grep | tr "\n" "|"' | cut -c1-900)"
    echo "  debug daemon.log:"; box "tail -40 /root/daemon.log" | sed 's/^/    /'
    echo "  debug logs:"; box "ls -la /root/.reminal/logs 2>/dev/null; for f in /root/.reminal/logs/*$2* /root/.reminal/*$2*.log; do [ -f \"\$f\" ] && { echo \"== \$f\"; tail -30 \"\$f\"; }; done" | sed 's/^/    /' | cut -c1-240
  fi
done
argv=$(box "grep -- '--model opus-late' /root/.fake-claude/argv.log | tail -1")
[ "$argv" = "argv: --model opus-late" ] && ok "late started as: claude $(echo "$argv" | cut -c7-) — its flags kept, no resume" || fail "late's last start: $argv"
argv=$(box "grep -- '--model opus-early' /root/.fake-claude/argv.log | tail -1")
[ "$argv" = "argv: --model opus-early" ] && ok "early started as: claude $(echo "$argv" | cut -c7-) — its flags kept, no resume" || fail "early's last start: $argv"
box "reminal kill $L -y >/dev/null 2>&1; reminal kill $E -y >/dev/null 2>&1"
[ $fails -eq 0 ] && echo PASS || { echo "$fails failure(s)"; exit 1; }
