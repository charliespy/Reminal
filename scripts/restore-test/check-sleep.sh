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
# shellin <dir>: the pid of the session shell (bash) working in dir.
shellin() { box "for p in \$(pgrep -x bash); do [ \"\$(readlink /proc/\$p/cwd)\" = $1 ] && echo \$p; done" | head -1; }
# ended <pid>: gone, or a zombie (ended, not yet reaped).
ended() { box "! grep -q '^State:[[:space:]]*[^Z]' /proc/$1/status 2>/dev/null"; }

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
SHPID=$(shellin /root/project)
[ -n "$SHPID" ] && ! ended $SHPID && ok "its shell is pid $SHPID, running" || fail "no running session shell found ($SHPID)"

# keep-awake holds it awake; --off lets it go.
box "reminal keep-awake $ID >/dev/null"
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "kept awake · you asked" && ok "kept awake: sleep refused ($(echo "$o" | head -1))" || fail "keep-awake not honoured: $o"
box "reminal keep-awake $ID --off >/dev/null"

# A sleep
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "asleep" && ok "slept: $o" || fail "sleep failed: $o"
running $ID && fail "$ID still running" || ok "its processes are gone"
[ "$(claudes)" -lt "$before" ] && ok "claude ended ($before → $(claudes) running)" || fail "claude still running"
[ -n "$SHPID" ] && ended $SHPID && ok "its shell ended (pid $SHPID: the terminal was hung up)" || fail "its shell $SHPID still running: $(box "grep State /proc/$SHPID/status")"
box "test -f /root/.reminal/restore/$ID.sealed && test -f /root/.reminal/restore/$ID.asleep" && ok "record kept, marked asleep" || fail "no record or no asleep mark"
box "reminal list" | grep -q "$ID.*asleep" && ok "reminal list shows it asleep" || fail "list: $(box 'reminal list')"
# ...and run inside the only other session, the one you are in: listed there too.
out=$(newsession here /root/here); HERE=$(idof "$out")
box "REMINAL_SESSION=$HERE reminal list" | grep -q "$ID.*asleep" && ok "reminal list inside the only session running shows it asleep" || fail "list inside $HERE: $(box "REMINAL_SESSION=$HERE reminal list")"
box "reminal kill $HERE -y >/dev/null 2>&1"
# The agent tools on a session asleep: listed, read and searched from what it
# saved, never an error.
box "mcp.sh list" | grep -q "$ID" && box "mcp.sh list" | grep -q 'asleep' && ok "list_sessions shows it, asleep" || fail "list_sessions: $(box 'mcp.sh list' | cut -c1-300)"
box "mcp.sh read $ID" | grep -q "before-the-sleep-marker" && ok "read_transcript reads what it saved" || fail "read_transcript: $(box 'mcp.sh read '$ID | cut -c1-300)"
box "mcp.sh search before-the-sleep-marker" | grep -q "$ID" && ok "search_sessions finds it in what it saved" || fail "search_sessions: $(box 'mcp.sh search before-the-sleep-marker' | cut -c1-300)"

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
box "reminal list" | grep "$ID" | grep -q "work" && ok "its name came back (work)" || fail "name lost: $(box 'reminal list')"
for i in $(seq 1 10); do box "mcp.sh list" | grep -qE 'attn.{0,8}done' && break; sleep 1; done
box "mcp.sh list" | grep -qE 'attn.{0,8}done' && ok "its agent's attention state works again (done)" || fail "no attention state after the wake: $(box 'mcp.sh list' | cut -c1-400)"

# A job running in it: refused, and said; --anyway lets it go, and says so.
send $ID "$(printf '\\u0004')"; sleep 1          # leave claude
send $ID 'sleep 600 &'; sleep 1
send $ID 'claude'; sleep 2; send $ID "hello again"; sleep 2
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "sleep, running in it" && echo "$o" | grep -q -- "--anyway" \
    && ok "a job in it refuses the sleep, and says what: $(echo "$o" | head -1)" || fail "job not refused: $o"
running $ID && ok "…and it is still awake" || fail "slept despite the job"
o=$(box "reminal sleep $ID --anyway 2>&1"); echo "$o" | grep -q "asleep" && ok "--anyway sleeps it" || fail "--anyway: $o"
box "reminal list" | grep -q "let go of: sleep, running in it" && ok "the list says what that let go of" || fail "dropped not shown: $(box 'reminal list')"
# send_keys to a session asleep wakes it, and the keys land in its agent.
box "MCP_WAIT=40 mcp.sh send $ID 'hello from the agent tools' >/dev/null"
sleep 2
running $ID && ok "send_keys woke it" || fail "send_keys did not wake it"
box "grep -rq 'you said: hello from the agent tools' /root/.fake-claude/" && ok "…and the keys went to claude, not the shell" || fail "keys lost: $(screen $ID | tail -5)"

# reminal connect to a session asleep wakes it (a terminal attaching is a
# person at it).
box "reminal sleep $ID --anyway >/dev/null 2>&1"
box "timeout 8 script -qec 'reminal connect $ID $PIN' /dev/null >/dev/null 2>&1; true"
running $ID && ok "reminal connect woke it" || fail "reminal connect did not wake it: $(box 'reminal list')"

# A terminal attached ACROSS a sleep waits, says it is asleep, and is back
# after the wake: never a misleading error and an exit.
docker exec -d $BOX sh -c "sleep 3600 | script -qfc 'reminal connect $ID $PIN' /tmp/conn.log"; sleep 4   # stdin open and empty: script's EOF would type logout
box "reminal sleep $ID --anyway >/dev/null 2>&1"; sleep 20
box "pgrep -f 'reminal connect $ID' >/dev/null" && ok "a terminal attached across the sleep is still there 20 s on" || fail "the attached terminal exited: $(box 'tail -c 400 /tmp/conn.log')"
box "grep -q 'it is asleep' /tmp/conn.log" && ok "…and says it is asleep" || fail "no asleep word: $(box 'tail -c 400 /tmp/conn.log')"
box "grep -q 'older than 3.15.6' /tmp/conn.log" && fail "the misleading old-version error" || ok "…no misleading error"
box "reminal wake $ID >/dev/null 2>&1"; sleep 15   # past the restore's wait before it types the resume
send $ID "echo back-after-the-wake-$$"; sleep 3
box "grep -q 'back-after-the-wake-' /tmp/conn.log" && ok "after the wake it is live in that terminal again" || fail "the attached terminal did not come back: woke=$(box 'grep -c "woke from sleep" /tmp/conn.log') tail=$(box 'tail -c 3500 /tmp/conn.log | cat -v | tr -d '\r'')"
box "pkill -f 'reminal connect $ID'; true"

# A plain shell: refused unless --anyway (what was typed by hand is lost).
o2=$(newsession plain /root); P=$(idof "$o2"); sleep 2
o=$(box "reminal sleep $P 2>&1"); echo "$o" | grep -q "typed into its shell by hand" && ok "a plain shell refuses: typed-by-hand state would go" || fail "plain shell: $o"

# What runs in front and is not the agent it wakes with is a job a sleep
# would end: a command in a plain shell, or one run after quitting claude
# (its record still holding claude, to wake with).
send $P 'sleep 611'; sleep 1
o=$(box "reminal sleep $P 2>&1"); echo "$o" | grep -q "sleep, running in it" && ok "a command in front of a shell refuses the sleep: $(echo "$o" | head -1)" || fail "command in front of a shell: $o"
send $P "$(printf '\\u0003')"
send $ID "$(printf '\\u0004')"; sleep 1          # leave claude
send $ID 'sleep 622'; sleep 2
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "sleep, running in it" && ! echo "$o" | grep -q "didn't say" && ok "a command run after quitting claude refuses the sleep: $(echo "$o" | head -1)" || fail "command after quitting claude: $o"
running $ID || fail "slept with a command in front"
send $ID "$(printf '\\u0003')"; sleep 1
send $ID 'claude'; sleep 2; send $ID "hello once more"; sleep 2

# Typed and not sent: a sleep would drop it, so it asks; cleared, it sleeps.
box "mcp.sh type $ID 'half a thought' >/dev/null"; sleep 1
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "typed and not sent" && ok "an unsent draft refuses the sleep: $(echo "$o" | head -1)" || fail "draft: $o"
box "mcp.sh type $ID '$(printf '\\u0015')' >/dev/null"; sleep 1
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "asleep" && ok "the draft cleared, it sleeps: $o" || fail "after clearing the draft: $o"

# Background work its agent left without saying (its hook says none): on its
# terminal and new since it came to rest, however soon after a wake, under
# it or reparented away.
box "reminal wake $ID >/dev/null 2>&1"
for i in $(seq 1 40); do [ "$(claudes)" -gt 0 ] && box "mcp.sh list" | grep -qE 'attn.{0,8}done' && break; sleep 1; done
send $ID 'bg'; sleep 2                           # seconds after the wake
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "sleep, left running in the background" && ok "a command left going right after a wake refuses the sleep: $(echo "$o" | head -1)" || fail "left going after a wake: $o; $(box "ps -eo pid,ppid,tty,comm" | tail -12)"
box "pgrep -f 'slee[p] 633' >/dev/null" && ok "…and it is still running" || fail "the command left going was ended"
box "pkill -f 'slee[p] 633'; true"; sleep 1
send $ID 'orphan'; sleep 2
op=$(box "ps -o ppid= -p \$(pgrep -f 'slee[p] 644' | head -1) | tr -d ' '")
[ -n "$op" ] && ! box "pgrep -x claude | grep -qx $op" && ok "control: the orphan's parent is $op, not claude" || fail "the orphan is not reparented (parent $op)"
o=$(box "reminal sleep $ID 2>&1"); echo "$o" | grep -q "sleep, left running in the background" && ok "a command reparented away from claude refuses the sleep: $(echo "$o" | head -1)" || fail "orphan: $o"
box "pkill -f 'slee[p] 644'; true"; sleep 1

# A conversation gone while asleep: it says so, never a silent fresh start.
CONV=$(box "ls -t /root/.fake-claude/*.conv | head -1 | xargs basename | sed 's/.conv$//'")   # the conversation it is in now
o=$(box "reminal sleep $ID --anyway 2>&1")
box "rm -f /root/.claude/projects/*/$CONV.jsonl"
o=$(box "reminal wake $ID 2>&1"); echo "$o" | grep -q "conversation $CONV is gone" && ok "a lost conversation: $o" || fail "wake with its conversation gone: $o"
box "reminal list" | grep -q "$ID.*asleep" && ok "…and it stays asleep" || fail "not asleep after a refused wake"

# An agent that never says what it left running (an older build): asked.
o3=$(newsession unsaid /root/unsaid); G=$(idof "$o3")
send $G 'FAKE_CLAUDE_BG_SAID=0 claude'; sleep 2; send $G "hi"; sleep 2
o=$(box "reminal sleep $G 2>&1"); echo "$o" | grep -q "it didn't say" && ok "an agent that never said what it left: $(echo "$o" | head -1)" || fail "unsaid background: $o"

# Ending a session asleep: reminal kill, as for one running; nothing left.
box "reminal sleep $G --anyway >/dev/null 2>&1"
o=$(box "reminal kill $G -y 2>&1"); echo "$o" | grep -q "Ended sleeping session $G" && ok "reminal kill ends a session asleep" || fail "kill a sleeper: $o"
box "ls /root/.reminal/restore/ | grep -q $G" && fail "something of it is left: $(box "ls /root/.reminal/restore/ | grep $G")" || ok "…its record and marks are gone"
o=$(box "reminal wake $G 2>&1"); echo "$o" | grep -q "awake" && fail "woke one that was ended: $o" || ok "…and it cannot be woken: $(echo "$o" | head -1)"

# A first turn begun before claude ever rested (claude "do X", keys typed as
# it starts): what that turn left going is still asked about.
box "pkill -f 'slee[p] 633'; true"
o4=$(newsession early /root/early); E1=$(idof "$o4"); sleep 1
send $E1 'claude bg'                       # its first turn on its command line: begun before it ever rests
for i in $(seq 1 20); do box "pgrep -f 'slee[p] 633' >/dev/null" && break; sleep 1; done; sleep 3
o=$(box "reminal sleep $E1 2>&1"); echo "$o" | grep -q "sleep, left running in the background" && ok "a command left going by a turn begun before claude rested refuses the sleep: $(echo "$o" | head -1)" || fail "first turn before a rest: $o"
box "pgrep -f 'slee[p] 633' >/dev/null" && ok "…and it is still running" || fail "the command left going by the first turn was ended"
box "pkill -f 'slee[p] 633'; true"

[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
