#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# What `reminal sleep` / `reminal wake` promise, against the rig run.sh
# starts: a session put to sleep frees its processes and keeps its record;
# woken, it is itself again (same id and PIN, its scrollback, claude on the
# same conversation). It refuses when a sleep would end something a wake
# can't bring back, and says what; --anyway lets that go and the list says
# so. Asleep, it stays asleep through a daemon restart and a reboot.
BOX=${BOX:-reminal-restore-box} RELAY=${RELAY:-reminal-restore-relay}
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
box()  { docker exec $BOX sh -c "$1"; }
send() { box "mcp.sh send $1 '$2' >/dev/null"; }
screen() { box "mcp.sh read $1"; }
newsession() { box "mkdir -p $2 && cd $2 && reminal new $1 2>&1"; }
idof()  { echo "$1" | sed -n 's/^ *Session: *\([A-Z0-9]*\).*/\1/p' | head -1; }
pinof() { echo "$1" | sed -n 's/.*PIN: *\([0-9]*\).*/\1/p'; }
running() { box "reminal list" | grep "$1" | grep -vq asleep; }
claudes() { box "pgrep -x claude | wc -l | tr -d ' '"; }

out=$(newsession work /root/project); ID=$(idof "$out"); PIN=$(pinof "$out")
[ -n "$ID" ] && ok "session $ID (PIN $PIN)" || { fail "no session: $out"; exit 1; }
send $ID "echo before-the-sleep-marker"
send $ID 'claude --model opus-test'
sleep 2
send $ID "remember the number 4242"
sleep 2
CONV=$(box "ls -t /root/.fake-claude/*.conv | head -1 | xargs basename | sed 's/.conv$//'")
screen $ID | grep -q "you said: remember the number 4242" && ok "claude is running, conversation $CONV" || fail "claude not answering"
before=$(claudes)

# keep-awake holds it awake; --off lets it go.
box "reminal keep-awake $ID >/dev/null"
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "kept awake · you asked" && ok "kept awake: sleep refused ($(echo "$o" | head -1))" || fail "keep-awake not honoured: $o"
box "reminal keep-awake $ID --off >/dev/null"

# A sleep
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "asleep" && ok "slept: $o" || fail "sleep failed: $o"
running $ID && fail "$ID still running" || ok "its processes are gone"
[ "$(claudes)" -lt "$before" ] && ok "claude ended ($before → $(claudes) running)" || fail "claude still running"
box "test -f /root/.reminal/restore/$ID.sealed && test -f /root/.reminal/restore/$ID.asleep" && ok "record kept, marked asleep" || fail "no record or no asleep mark"
box "reminal list" | grep -q "$ID.*asleep" && ok "reminal list shows it asleep" || fail "list: $(box 'reminal list')"

# Asleep through a daemon restart and a reboot.
box "pkill -x -f 'reminal daemon'; sleep 1; (reminal daemon >/root/daemon2.log 2>&1 &)"; sleep 6
box "pgrep -x -f 'reminal daemon' >/dev/null" && ok "the daemon restarted" || fail "no daemon after the restart"
running $ID && fail "a daemon restart woke it" || ok "a daemon restart leaves it asleep"
docker restart $BOX >/dev/null; sleep 8
running $ID && fail "a reboot woke it" || ok "a reboot leaves it asleep"

# A wake
o=$(box "reminal wake $ID 2>&1"); echo "$o" | grep -q "awake" && ok "woke: $o" || fail "wake failed: $o"
for i in $(seq 1 20); do box "grep -q -- '--resume' /root/.fake-claude/argv.log" 2>/dev/null && break; sleep 1; done
sleep 3
running $ID && ok "running again, same id $ID" || fail "not running after wake"
box "test -f /root/.reminal/restore/$ID.asleep" && fail "still marked asleep" || ok "no longer marked asleep"
s=$(screen $ID)
echo "$s" | grep -q "before-the-sleep-marker" && ok "scrollback from before the sleep is there" || fail "scrollback lost"
echo "$s" | grep -q "fake-claude: resumed conversation $CONV" && ok "claude resumed the same conversation" || fail "claude not resumed: $(echo "$s" | tail -4)"
echo "$s" | grep -q "(earlier) you said: remember the number 4242" && ok "…with what was said in it" || fail "conversation content missing"
send $ID "what number"; sleep 1
box "grep -q 'you said: what number' /root/.fake-claude/$CONV.conv" && ok "new input goes into the same conversation" || fail "new input went elsewhere"
docker exec -e REMINAL_RELAY=ws://$RELAY:8080/ws $RELAY sh -c "mcp.sh read 'http://$RELAY:8080/?s=$ID#p=$PIN'" | grep -q "what number" \
    && ok "reachable through the relay with its old id and PIN" || fail "old link and PIN don't reach it"

# A job running in it: refused, and said; --anyway lets it go, and says so.
send $ID "$(printf '\\u0004')"; sleep 1          # leave claude
send $ID 'sleep 600 &'; sleep 1
send $ID 'claude'; sleep 2; send $ID "hello again"; sleep 2
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "sleep, running in it" && echo "$o" | grep -q -- "--anyway" \
    && ok "a job in it refuses the sleep, and says what: $(echo "$o" | head -1)" || fail "job not refused: $o"
running $ID && ok "…and it is still awake" || fail "slept despite the job"
o=$(box "reminal sleep $ID --anyway 2>&1"); echo "$o" | grep -q "asleep" && ok "--anyway sleeps it" || fail "--anyway: $o"
box "reminal list" | grep -q "let go of: sleep, running in it" && ok "the list says what that let go of" || fail "dropped not shown: $(box 'reminal list')"
box "reminal wake $ID >/dev/null 2>&1"; sleep 4

# A plain shell: refused unless --anyway (what was typed by hand is lost).
o2=$(newsession plain /root); P=$(idof "$o2"); sleep 2
o=$(box "reminal sleep $P 2>&1"); echo "$o" | grep -q "typed into its shell by hand" && ok "a plain shell refuses: typed-by-hand state would go" || fail "plain shell: $o"

# A conversation gone while asleep: it says so, never a silent fresh start.
CONV=$(box "ls -t /root/.fake-claude/*.conv | head -1 | xargs basename | sed 's/.conv$//'")   # the conversation it is in now
o=$(box "reminal sleep $ID --anyway 2>&1")
box "rm -f /root/.claude/projects/*/$CONV.jsonl"
o=$(box "reminal wake $ID 2>&1"); echo "$o" | grep -q "conversation $CONV is gone" && ok "a lost conversation: $o" || fail "wake with its conversation gone: $o"
box "reminal list" | grep -q "$ID.*asleep" && ok "…and it stays asleep" || fail "not asleep after a refused wake"

[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
