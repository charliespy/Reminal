#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# One idle session told to stop on its own (run.sh stop): SIGTERM to its
# process alone, as a stop or an upgrade sends it, with nothing else on
# the machine signalled. It ends within seconds, its shell hung up as a
# closed terminal hangs it up, and its restore record is kept. A job its
# person started to outlive the terminal (nohup) goes on, and so does
# every other session.
BOX=${BOX:-reminal-restore-box}
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
box()  { docker exec $BOX sh -c "$1"; }
send() { box "mcp.sh send $1 '$2' >/dev/null"; }
newsession() { box "mkdir -p $2 && cd $2 && reminal new $1 2>&1"; }
idof()  { echo "$1" | sed -n 's/^ *Session: *\([A-Z0-9]*\).*/\1/p' | head -1; }
pidof() { box "sed -n 's/.*\"pid\": *\([0-9]*\).*/\1/p' /root/.reminal/active-$1.json | head -1"; }
# Running: there, and not a zombie (State Z: ended, not yet reaped). The
# box has an init (run.sh: --init), so an ended process does not linger.
state() { box "sed -n 's/^State:[[:space:]]*\([A-Z]\).*/\1/p' /proc/$1/status 2>/dev/null"; }
alive() { s=$(state $1); [ -n "$s" ] && [ "$s" != Z ]; }

# With no init as PID 1, an ended session stays a zombie (State Z) until
# reaped, and could read as running: the box must have one.
p1=$(box 'cat /proc/1/comm')
case "$p1" in
docker-init|tini|init|systemd) ok "PID 1 is $p1" ;;
*) fail "PID 1 is $p1, no init: run the box with --init (run.sh does)"; exit 1 ;;
esac

one=$(idof "$(newsession one /root/one)")
other=$(idof "$(newsession other /root/other)")
[ -n "$one" ] && [ -n "$other" ] || { fail "sessions not started"; exit 1; }
ok "sessions $one and $other"
send $one 'nohup sleep 600 </dev/null >/dev/null 2>&1 & echo kept-$!'
sleep 2
kept=$(box "mcp.sh read $one" | sed -n 's/.*kept-\([0-9][0-9]*\).*/\1/p' | tail -1)
[ -n "$kept" ] || fail "its nohup job not started"

# Idle, and its restore record written (the first save).
for i in $(seq 1 60); do box "test -s /root/.reminal/restore/$one.sealed" && break; sleep 1; done
box "test -s /root/.reminal/restore/$one.sealed" || fail "no restore record before the stop"
pid=$(pidof $one)
opid=$(pidof $other)
shell=$(box "ps -o pid= --ppid $pid | head -1" | tr -d ' ')
[ -n "$pid" ] && [ -n "$shell" ] || { fail "its process ($pid) or shell ($shell) not found"; exit 1; }
sleep 2

box "kill -TERM $pid"
gone=
for i in $(seq 1 50); do
    if ! alive $pid; then gone=$i; break; fi
    sleep 0.1
done
echo "     its process $pid: state '$(state $pid)', its shell $shell: state '$(state $shell)', PID 1: $(box 'cat /proc/1/comm')"
if [ -n "$gone" ]; then ok "SIGTERM alone ended it, in about ${gone}00 ms"; else fail "SIGTERM alone: still running 5 s later"; fi
if alive $shell; then fail "its shell ($shell) still running"; else ok "its shell hung up"; fi
if box "test -s /root/.reminal/restore/$one.sealed"; then ok "its restore record kept"; else fail "its restore record lost"; fi
if [ -n "$kept" ] && alive $kept; then ok "its nohup job goes on"; else fail "its nohup job ($kept) ended"; fi
if alive $opid; then ok "the other session goes on"; else fail "the other session ($opid) ended"; fi
box "kill -TERM $pid 2>/dev/null; kill -KILL $pid $shell 2>/dev/null; true"

[ $fails -eq 0 ] && echo PASS || { echo "$fails failed"; exit 1; }
