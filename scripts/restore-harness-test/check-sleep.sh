#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Sleep and wake with the real coding agents. Each that sleeps (it reports
# its conversation and is resumed by it) is put to sleep mid-conversation by
# a plain `reminal sleep` — its own MCP servers and what launched them are
# not work it left — its processes end, and woken it is back on exactly its
# conversation, answers in it, and sleeps again. One that would wake on
# "the latest" only (gemini) is kept awake and says why.
. "$(dirname "$0")/lib.sh"
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
skip() { echo "skip $*"; }
setid() { eval "ID_$1=$2"; }
id()    { eval "echo \$ID_$1"; }
cmd_of() { case $1 in codex) echo "codex ${CODEX_EXTRA:-}" ;; *) echo $1 ;; esac; }
asleep() { bx "reminal list 2>/dev/null" | grep "$1" | grep -q asleep; }

SLEEPERS=${SLEEPERS:-"claude qwen codex opencode pi"}
for s in ${SKIP_AGENTS:-}; do skip "$s: cannot run on this box"; SLEEPERS=$(echo " $SLEEPERS " | sed "s/ $s / /" | tr -s ' '); done
ALL="$SLEEPERS gemini"
for a in $ALL; do setid $a "$(newsess z$a $H/z-$a)"; send $(id $a) "$(cmd_of $a)"; done
for a in $ALL; do ready $(id $a) $a; done
for a in $ALL; do send $(id $a) "remember the number 4242"; done
for a in $ALL; do
    eventually 90 shows $(id $a) "noted: remember the number 4242" && ok "$a ($(id $a)) is in a conversation" || fail "$a: no answer"
done
for a in $SLEEPERS; do eventually 40 has_conv $(id $a); done
sleep 5   # its MCP servers come up behind its prompt

o=$(bx "reminal sleep $(id gemini) 2>&1")
printf '%s\n' "$o" | grep -q "gemini can't be resumed exactly yet" && ok "gemini is kept awake: $o" || fail "gemini: $o"
for a in $SLEEPERS; do
    sid=$(id $a) conv=$(restore_field $sid conv)
    o=$(bx "reminal sleep $sid 2>&1")
    printf '%s\n' "$o" | grep -q "$sid asleep" && ok "$a: slept with a plain sleep on conversation $conv" || { fail "$a: $o"; continue; }
    eval "CONV_$a=$conv"
done
sleep 3
for a in $SLEEPERS; do
    sid=$(id $a)
    asleep $sid || continue
    # its agent (codex's machine-wide app-server is its own, not the session's)
    left=$(args_in $H/z-$a "$a" | grep -v app-server)
    [ -z "$left" ] && ok "$a: its agent ended" || fail "$a: still running: $left"
    o=$(bx "reminal wake $sid 2>&1")
    printf '%s\n' "$o" | grep -q "awake" && ok "$a: woke" || fail "$a: wake: $o"
done
for a in $SLEEPERS; do
    sid=$(id $a)
    eventually 120 shows_after $sid "remember the number 4242" && ok "$a: back on its conversation" || fail "$a: its conversation is not back"
    since_restore $sid | tr '\n' ' ' | grep -q "$(eval echo \$CONV_$a)" && ok "$a: woken on its own id" || fail "$a: not woken on $(eval echo \$CONV_$a)"
done
for a in $SLEEPERS; do send $(id $a) "what number"; done
for a in $SLEEPERS; do
    sid=$(id $a)
    eventually 90 shows_after $sid "noted: what number" && ok "$a: answers in it" || fail "$a: no answer after the wake"
    o=$(bx "reminal sleep $sid 2>&1")
    printf '%s\n' "$o" | grep -q "$sid asleep" && ok "$a: sleeps again after a turn" || fail "$a: again: $o"
done
[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
