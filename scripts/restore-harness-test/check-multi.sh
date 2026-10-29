#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Several conversations with the same agent in the same folder, all ended by
# one reboot — where "the latest conversation here" would be the same one for
# all of them. Each session is told its own number. After the restore:
#   by id   (claude, qwen, pi, opencode — they report it): each is back on
#           ITS conversation, showing its number and no other session's;
#   picker  (codex, cursor; one that never got to report an id): the agent's own
#           list is open to choose from, nothing resumed by guess;
#   afresh  (a claude never sent anything, its id said as it started): it is
#           started anew, and a line says so;
#   neither (gemini; opencode with no id): the agent is not started, and a
#           line says how to find the conversation.
# Run in small batches — every session is a real agent: PLAN="claude:3 qwen:2".
. "$(dirname "$0")/lib.sh"
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
cmd_of() { case $1 in cursor) echo cursor-agent ;; codex) echo "codex ${CODEX_EXTRA:-}" ;; *) echo $1 ;; esac; }

PLAN=${PLAN:-"claude:3 qwen:2"}
NOID=${NOID:-}          # an agent to add one more session of, never spoken to
ALL=""
for p in $PLAN; do
    a=${p%%:*} n=${p#*:}
    i=1
    while [ $i -le $n ]; do
        id=$(newsess $a$i $H/shared-$a)
        eval "S_$a$i=$id A_$a$i=$a"; ALL="$ALL $a$i"
        send $id "$(cmd_of $a)"
        i=$((i+1))
    done
done
if [ -n "$NOID" ]; then
    id=$(newsess ${NOID}silent $H/shared-$NOID)
    eval "S_${NOID}silent=$id A_${NOID}silent=$NOID"
    send $id "$(cmd_of $NOID)"
fi
for s in $ALL ${NOID:+${NOID}silent}; do ready $(eval echo \$S_$s) $(cmd_of $(eval echo \$A_$s) | cut -d' ' -f1); done
num=100
for s in $ALL; do
    num=$((num+1)); eval "N_$s=$num"
    send $(eval echo \$S_$s) "my number is $num"
    # one after another, so "latest" is well defined
    eventually 60 shows $(eval echo \$S_$s) "noted: my number is $num"
done
for s in $ALL; do
    id=$(eval echo \$S_$s) n=$(eval echo \$N_$s)
    shows $id "noted: my number is $n" && ok "$s ($id) holds conversation #$n" || fail "$s: no answer before the reboot"
done
for s in $ALL ${NOID:+${NOID}silent}; do eventually 40 saved $(eval echo \$S_$s) $(cmd_of $(eval echo \$A_$s) | cut -d' ' -f1); done
reboot_box || exit 1
ok "rebooted"
want=$(echo $ALL ${NOID:+x} | wc -w | tr -d ' ')
eventually 240 enough_sessions $want
# Settled: each session has been restored and has typed what it will type.
for s in $ALL ${NOID:+${NOID}silent}; do eventually 90 shows_after $(eval echo \$S_$s) "restored this session"; done
sleep 15
procs=$(procs)
# picker_open ID REGEX TEXT — the agent's own list is open: its process by
# REGEX (Linux, macOS), or on Windows what reminal typed.
# pi rewrites its process title, so its list is told by what was typed too.
picker_open() { if [ "$T" = win ] || [ "${3%% *}" = pi ]; then typed "$1" "$3"; else printf '%s\n' "$procs" | grep -qE "$2"; fi; }
agent_running() {
    if [ "$T" = win ]; then printf '%s\n' "$procs" | grep -vi reminal | grep -qiE "[\\/]$1([.-]|\\|/| |\$)"
    else printf '%s\n' "$procs" | grep -qE "(^| |/)$1( |\$)"; fi
}

# picker_of AGENT — what opens AGENT's own list of conversations.
picker_of() { case $1 in codex) echo "codex resume" ;; cursor) echo "cursor-agent --resume" ;; *) echo "$1 --resume" ;; esac; }

check_session() { # name agent id number
    # Joined into one line: the terminal wraps a note where it likes.
    s=$1 a=$2 id=$3 n=$4 out=$(since_restore $3) flat=$(since_restore $3 | tr '\n' ' ' | tr -s ' ')
    case $a in
    claude|qwen|pi|opencode)
        if [ -z "$n" ] && [ $a = opencode ]; then    # never spoke, and no list to open
            printf '%s\n' "$flat" | grep -q "to find this one's conversation" && ok "$s: never reported an id — not started; the note says how to find it" || fail "$s: no note"
            return
        fi
        if [ -z "$n" ] && [ $a = claude ]; then    # never spoke, its id said at start (SessionStart): nothing to resume
            printf '%s\n' "$flat" | grep -q "had not been sent anything: started afresh" && ok "$s: never sent anything — started afresh, and says so" || fail "$s: no started-afresh note"
            printf '%s\n' "$out" | grep -q "noted: my number" && fail "$s: resumed someone's conversation" || ok "$s: no one's conversation resumed by guess"
            return
        fi
        if [ -z "$n" ]; then    # never spoke: nothing to resume by id
            pick=$(picker_of $a)
            printf '%s\n' "$flat" | grep -q "pick this session's" && ok "$s: never reported an id — told to pick" || fail "$s: no pick note"
            picker_open $id "(^|/)$pick\$" "$pick" && ok "$s: $a's own list opened ($pick)" || fail "$s: picker not opened"
            printf '%s\n' "$out" | grep -q "noted: my number" && fail "$s: resumed someone's conversation" || ok "$s: no one's conversation resumed by guess"
            return
        fi
        got=$(printf '%s\n' "$out" | grep -o "my number is [0-9]*" | sort -u | tr '\n' ' ')
        [ "$got" = "my number is $n " ] && ok "$s: back on its own conversation (#$n) — none of the others" || fail "$s: expected #$n, shows: '$got'" ;;
    codex|cursor)
        pick=$(picker_of $a)
        printf '%s\n' "$flat" | grep -q "pick this session's" && ok "$s: told to pick" || fail "$s: no pick note"
        picker_open $id "$pick\$" "$pick" && ok "$s: $a's own list opened ($pick)" || fail "$s: picker not opened"
        printf '%s\n' "$out" | grep -q "noted: my number" && fail "$s: resumed a conversation by guess" || ok "$s: nothing resumed by guess" ;;
    *)
        printf '%s\n' "$flat" | grep -q "to find this one's conversation" && ok "$s: not started; the note says how to find it" || fail "$s: no note"
        printf '%s\n' "$out" | grep -q "noted: my number" && fail "$s: resumed a conversation by guess" || ok "$s: nothing resumed by guess" ;;
    esac
}
for s in $ALL ${NOID:+${NOID}silent}; do
    check_session $s $(eval echo \$A_$s) $(eval echo \$S_$s) "$(eval echo \${N_$s:-})"
done
case "$PLAN" in *gemini*)
    for a in gemini; do
        case "$PLAN" in *$a:*) agent_running $a && fail "$a: started although it could not know whose conversation" || ok "$a: not started in any of its sessions" ;; esac
    done ;;
esac
[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; for s in $ALL; do echo "----- $s"; since_restore $(eval echo \$S_$s) | tail -8; done; exit 1; }
