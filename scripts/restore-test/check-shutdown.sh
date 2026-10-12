#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Several sessions, each with its own claude, through a machine shutting down
# (run.sh shutdown [MODE]): every one comes back with its claude resumed on
# its own conversation — or, where it cannot be, a line saying why. And one
# whose claude a person quit before the shutdown comes back without it.
#
#   together   every process told to stop at once (SIGTERM), as a machine
#              shutting down tells them, a moment to go, then the reboot
#   agents-5   the claudes stop first, the rest 5 s later
#   agents-45  the claudes stop first, the rest 45 s later (past the grace
#              a prompt keeps naming the agent that just left it)
BOX=${BOX:-reminal-restore-box}
MODE=${1:-together}
N=${N:-6}
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
box()  { docker exec $BOX sh -c "$1"; }
send() { box "mcp.sh send $1 '$2' >/dev/null"; }
screen() { box "mcp.sh read $1"; }
newsession() { box "mkdir -p $2 && cd $2 && reminal new $1 2>&1"; }
idof()  { echo "$1" | sed -n 's/^ *Session: *\([A-Z0-9]*\).*/\1/p' | head -1; }
pinof() { echo "$1" | sed -n 's/^ *PIN: *\([0-9]*\).*/\1/p' | head -1; }
convof() { screen $1 | sed -nE 's/.*fake-claude: (new|resumed) conversation (conv-[0-9a-f]+).*/\2/p' | tail -1; }

ids=""
for i in $(seq 1 $N); do
    id=$(idof "$(newsession s$i /root/p$i)")
    [ -n "$id" ] || { fail "session $i not started"; exit 1; }
    ids="$ids $id"
done
ok "$N sessions:$ids"
for id in $ids; do send $id 'claude --dangerously-skip-permissions'; done
sleep 3
for id in $ids; do send $id "words for $id"; done
sleep 2
convs=""
for id in $ids; do c=$(convof $id); [ -n "$c" ] || fail "$id: claude not running"; convs="$convs $id=$c"; done
ok "each runs claude:$convs"
# And one whose claude a person quits (typing, as at a viewer) before the
# machine goes down: it must not come back.
qo=$(newsession quitter /root/q); Q=$(idof "$qo"); QP=$(pinof "$qo")
send $Q 'claude --dangerously-skip-permissions'
sleep 20   # seen running by a save
box "mcp.sh sendpin $Q $QP '$(printf '\\u0004')' >/dev/null"
sleep 2
ok "a person typed ^D to claude in $Q"
sleep 47   # past the grace a prompt keeps naming the agent that just left it

case "$MODE" in
together)  box 'kill -TERM -1 2>/dev/null; sleep 5' || true ;;
agents-5)  box 'pkill -TERM -x claude; sleep 5; kill -TERM -1 2>/dev/null; sleep 5' || true ;;
agents-45) box 'pkill -TERM -x claude; sleep 45; kill -TERM -1 2>/dev/null; sleep 5' || true ;;
*) echo "unknown mode $MODE" >&2; exit 2 ;;
esac
docker restart $BOX >/dev/null
ok "shut down ($MODE), then rebooted"
# Restored one after another, each agent typed once its shell is quiet.
for i in $(seq 1 60); do
    n=$(box "grep -c -- '--resume' /root/.fake-claude/argv.log 2>/dev/null || echo 0")
    [ "$n" -ge "$N" ] && break
    sleep 1
done
sleep 3
back=0
for pair in $convs; do
    id=${pair%%=*} c=${pair#*=}
    s=$(screen $id)
    if echo "$s" | grep -q "fake-claude: resumed conversation $c"; then
        back=$((back+1))
    elif echo "$s" | tail -4 | grep -qiE "claude.*(could not|not resumed|was running)"; then
        ok "$id: not resumed, and says why: $(echo "$s" | grep -iE 'claude.*(could not|not resumed|was running)' | tail -1)"
    else
        fail "$id: claude not resumed, and nothing said: $(echo "$s" | tail -3 | tr '\n' ' ')"
    fi
done
[ $back -eq $N ] && ok "all $N claudes resumed on their own conversations" || echo "     $back of $N resumed"
qs=$(screen $Q)
# Its lines as lines (the screen comes back as one JSON string): only what
# is after the restore's banner counts.
if printf '%s' "$qs" | awk '{gsub(/\\n/, "\n"); print}' | sed -n '/restored this session/,$p' | grep -q "fake-claude:"; then
    fail "$Q: the claude a person quit came back: $(printf '%s' "$qs" | awk '{gsub(/\\n/, "\n"); print}' | sed -n '/restored this session/,$p' | head -5 | tr '\n' '|')"
else
    ok "$Q: the claude a person quit before the shutdown stayed quit"
fi
box "tail -n $N /root/.fake-claude/argv.log" | sed 's/^/     /'

[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
