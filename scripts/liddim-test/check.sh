#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Runs INSIDE a macOS VM (run.sh puts it there): the capture helper's spawn
# environment and path, and the lid dimmer's edge cases, against one build set
# in /tmp/liddim-test/<set>/ (reminal-dev, reminal-rel stamped 9.9.9 so it never
# fetches a release, reminal-capture-test with REMINAL_TEST_HOOKS, and
# reminal-capture-rel without). A VM has no lid or backlight: the test helper
# reads both from files (REMINAL_LIDDIM_FAKE).
SET=$1; B=/tmp/liddim-test/$SET; FAKEH=/tmp/liddim-test/fake/fake-helper; LOG=/tmp/liddim-test/calls.log
fails=0; pass(){ echo "PASS  $1"; }; fail(){ echo "FAIL  $1"; fails=$((fails+1)); }
waitfor(){ for _ in $(seq $2); do eval "$1" && return 0; sleep 0.5; done; return 1; }
isf(){ awk -v a="$1" -v b="$2" 'BEGIN{exit !(a+0==b+0)}'; }
clean(){ pkill -9 -f "/tmp/liddim-test/.+/(reminal|fake-helper)" 2>/dev/null; pkill -9 -f "T/tmp\.[A-Za-z0-9]*/Applications/reminal.app" 2>/dev/null; sleep 0.5; }
newhome(){ export HOME=$(mktemp -d); mkdir -p $HOME/.reminal; echo '{"closed_lid":true}' > $HOME/.reminal/settings.json; cd $HOME; }
base(){ export REMINAL_OWNERS_DIR=/tmp/liddim-test/etc REMINAL_RELAY=ws://127.0.0.1:1 REMINAL_WEB=http://127.0.0.1:1 SHELL=/bin/bash; }
base

echo "== a: the loader's variables never reach a helper"
clean; rm -f $LOG; newhome
env DYLD_FOO=1 LD_FOO=1 REMINAL_CAPTURE_HELPER=$FAKEH $B/reminal-dev new a >/dev/null 2>&1
waitfor "grep -q liddim $LOG 2>/dev/null" 40
line=$(grep liddim $LOG | head -1)
[ -n "$line" ] || fail "a: helper never started"
echo "$line" | grep -q -E "DYLD_FOO|LD_FOO" && fail "a: helper got: ${line#*| }" || pass "a: helper env: ${line#*| }"
echo "$line" | grep -q "REMINAL_CAPTURE_HELPER" && fail "a: helper got REMINAL_CAPTURE_HELPER" || pass "a: no REMINAL_CAPTURE_HELPER"

echo "== b: a release build runs only the helper it shipped with"
clean; rm -f $LOG; newhome
REMINAL_CAPTURE_HELPER=$FAKEH $B/reminal-rel new b1 >/dev/null 2>&1; sleep 16
grep -q liddim $LOG 2>/dev/null && fail "b: override outside the bundle was run" || pass "b: override outside the bundle not run"
clean; rm -f $LOG; newhome; mkdir -p /tmp/liddim-test/pathbin; cp $FAKEH /tmp/liddim-test/pathbin/reminal-capture
PATH=/tmp/liddim-test/pathbin:$PATH $B/reminal-rel new b2 >/dev/null 2>&1; sleep 16
grep -q liddim $LOG 2>/dev/null && fail "b: reminal-capture on PATH was run" || pass "b: PATH not searched"
# control: the shipped layout still works — the helper next to reminal in a bundle
clean; rm -f $LOG; newhome; A=/tmp/liddim-test/app/reminal.app/Contents/MacOS; rm -rf /tmp/liddim-test/app; mkdir -p $A; cp $B/reminal-rel $A/reminal; cp $FAKEH $A/reminal-capture
$A/reminal new b3 >/dev/null 2>&1; waitfor "grep -q liddim $LOG 2>/dev/null" 40 && pass "b: bundled helper runs" || fail "b: bundled helper not run"
clean; rm -f $LOG; newhome
REMINAL_CAPTURE_HELPER=$A/reminal-capture $A/reminal new b4 >/dev/null 2>&1; waitfor "grep -q liddim $LOG 2>/dev/null" 40 && pass "b: override inside the bundle runs" || fail "b: override inside the bundle refused"
rm -rf /tmp/liddim-test/app /tmp/liddim-test/pathbin

echo "== c: a saved brightness that isn't one is not put back"
F=/tmp/liddim-test/f; ST=/tmp/liddim-test/state
for v in nan inf 5 -1 0 abc; do
  clean; rm -rf $F; mkdir -p $F; echo 0 > $F/brightness; echo 0 > $F/lid; echo "$v" > $ST
  (REMINAL_LIDDIM_FAKE=$F $B/reminal-capture-test liddim $ST </dev/null >/dev/null 2>&1 &); sleep 3
  b=$(cat $F/brightness); if isf "$b" 0.5 && [ ! -e $ST ]; then pass "c: record '$v', dark screen on open -> 0.5"; else fail "c: record '$v' -> brightness '$b'"; fi
done
rm -f $ST

echo "== d1: SIGINT and SIGHUP restore like SIGTERM"
for sig in INT HUP; do
  clean; rm -rf $F $ST; mkdir -p $F; echo 0.6 > $F/brightness; echo 1 > $F/lid; mkfifo /tmp/liddim-test/in.$sig 2>/dev/null
  (REMINAL_LIDDIM_FAKE=$F $B/reminal-capture-test liddim $ST < /tmp/liddim-test/in.$sig >/dev/null 2>&1 &); exec 7>/tmp/liddim-test/in.$sig
  waitfor 'isf "$(cat $F/brightness)" 0' 10; kill -$sig $(pgrep -f "capture-test liddim")
  waitfor '[ -z "$(pgrep -f "capture-test liddim")" ]' 10; isf "$(cat $F/brightness)" 0.6 && pass "d1: SIG$sig -> 0.6" || fail "d1: SIG$sig -> $(cat $F/brightness)"
  exec 7>&-; rm -f /tmp/liddim-test/in.$sig
done

echo "== release helper ignores the fake lid"
clean; rm -rf $F $ST; mkdir -p $F; echo 0.6 > $F/brightness; echo 1 > $F/lid; mkfifo /tmp/liddim-test/in.rel 2>/dev/null
(REMINAL_LIDDIM_FAKE=$F $B/reminal-capture-rel liddim $ST < /tmp/liddim-test/in.rel > /tmp/liddim-test/rel.out 2>&1 &); exec 7>/tmp/liddim-test/in.rel; sleep 4
isf "$(cat $F/brightness)" 0.6 && [ ! -e $ST ] && pass "release helper: fake untouched ($(head -1 /tmp/liddim-test/rel.out))" || fail "release helper used the fake: brightness $(cat $F/brightness)"
exec 7>&-; rm -f /tmp/liddim-test/in.rel
echo "== say() with stdout a pipe"
clean; rm -rf $F $ST; mkdir -p $F; echo 0.6 > $F/brightness; echo 1 > $F/lid
out=$( (sleep 4 | REMINAL_LIDDIM_FAKE=$F $B/reminal-capture-test liddim $ST) 2>&1 | head -3 | tr '\n' ' ')
echo "$out" | grep -q "liddim up" && ! echo "$out" | grep -q Exception && pass "piped stdout: $out" || fail "piped stdout: $out"

echo "== d2: a stopped dimmer is ended and the screen put back"
clean; rm -rf $F; mkdir -p $F; echo 0.6 > $F/brightness; echo 1 > $F/lid; newhome
REMINAL_LIDDIM_FAKE=$F REMINAL_CAPTURE_HELPER=$B/reminal-capture-test $B/reminal-dev new d2 >/dev/null 2>&1
waitfor '[ -n "$(pgrep -f "capture-test liddim")" ]' 40; waitfor 'isf "$(cat $F/brightness)" 0' 10
P=$(pgrep -f "capture-test liddim"); kill -STOP $P; echo 0 > $F/lid
# lid opens while it is stopped: it can't answer; the agent must end it within a tick or two
waitfor 'isf "$(cat $F/brightness)" 0.6' 60 && pass "d2: stopped dimmer replaced, lid open -> 0.6" || fail "d2: still $(cat $F/brightness) after 30s"
ps -p $P -o stat= >/dev/null 2>&1 && fail "d2: stopped pid $P still there" || pass "d2: stopped pid gone"

echo "== d3: the session ends right after its dimmer died: the record is put back"
clean; rm -rf $F; mkdir -p $F; echo 0.6 > $F/brightness; echo 1 > $F/lid; newhome
REMINAL_LIDDIM_FAKE=$F REMINAL_CAPTURE_HELPER=$B/reminal-capture-test $B/reminal-dev new d3 >/dev/null 2>&1
waitfor '[ -n "$(pgrep -f "capture-test liddim")" ]' 40; waitfor 'isf "$(cat $F/brightness)" 0' 10
kill -9 $(pgrep -f "capture-test liddim"); $B/reminal-dev kill -y d3 >/dev/null 2>&1
waitfor '[ -z "$(pgrep -f "reminal-dev --headless")" ]' 20
sleep 2; isf "$(cat $F/brightness)" 0.6 && pass "d3: session ended -> 0.6" || fail "d3: still $(cat $F/brightness) after the session ended"
echo "== e: the dimmer follows the setting (control: off does nothing)"
clean; rm -rf $F; mkdir -p $F; echo 0.6 > $F/brightness; echo 0 > $F/lid; newhome; echo '{"closed_lid":false}' > $HOME/.reminal/settings.json
REMINAL_LIDDIM_FAKE=$F REMINAL_CAPTURE_HELPER=$B/reminal-capture-test $B/reminal-dev new e >/dev/null 2>&1; sleep 15
echo 1 > $F/lid; sleep 3
[ -z "$(pgrep -f "capture-test liddim")" ] && isf "$(cat $F/brightness)" 0.6 && pass "e: mode off: no dimmer, 0.6 with the lid shut" || fail "e: mode off: br=$(cat $F/brightness)"
echo '{"closed_lid":true}' > $HOME/.reminal/settings.json
waitfor 'isf "$(cat $F/brightness)" 0' 40 && pass "e: mode on, lid shut: 0" || fail "e: mode on: $(cat $F/brightness)"
echo 0 > $F/lid; waitfor 'isf "$(cat $F/brightness)" 0.6' 10 && pass "e: lid open: 0.6" || fail "e: open: $(cat $F/brightness)"
echo 1 > $F/lid; waitfor 'isf "$(cat $F/brightness)" 0' 10; echo '{"closed_lid":false}' > $HOME/.reminal/settings.json
waitfor '[ -z "$(pgrep -f "capture-test liddim")" ] && isf "$(cat $F/brightness)" 0.6' 40 && pass "e: mode off with the lid shut: 0.6, dimmer gone" || fail "e: off: br=$(cat $F/brightness)"
$B/reminal-dev kill -y e >/dev/null 2>&1

echo "== f: reminal-overlay gets the same treatment (via reminal mcp list_windows)"
mcp(){ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}' '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_windows","arguments":{}}}' | "$@" mcp >/dev/null 2>&1; }
clean; rm -f $LOG; newhome
(export DYLD_FOO=1 LD_FOO=1 REMINAL_OVERLAY_BIN=$FAKEH; mcp $B/reminal-dev)
line=$(grep " windows" $LOG | head -1)
[ -n "$line" ] || fail "f: overlay never started"
echo "$line" | grep -q -E "DYLD_FOO|LD_FOO|REMINAL_OVERLAY_BIN" && fail "f: overlay got: ${line#*| }" || pass "f: overlay env: ${line#*| }"
clean; rm -f $LOG; newhome
REMINAL_OVERLAY_BIN=$FAKEH mcp $B/reminal-rel
grep -q " windows" $LOG 2>/dev/null && fail "f: release ran an overlay override outside the bundle" || pass "f: release: override outside the bundle not run"
rm -f $LOG; mkdir -p /tmp/liddim-test/pathbin; cp $FAKEH /tmp/liddim-test/pathbin/reminal-overlay
PATH=/tmp/liddim-test/pathbin:$PATH mcp $B/reminal-rel
grep -q " windows" $LOG 2>/dev/null && fail "f: release ran reminal-overlay from PATH" || pass "f: release: PATH not searched"
rm -f $LOG; A=/tmp/liddim-test/app/reminal.app/Contents/MacOS; rm -rf /tmp/liddim-test/app; mkdir -p $A; cp $B/reminal-rel $A/reminal; cp $FAKEH $A/reminal-overlay
mcp $A/reminal; grep -q " windows" $LOG && pass "f: release: bundled overlay runs" || fail "f: release: bundled overlay not run"
rm -rf /tmp/liddim-test/app /tmp/liddim-test/pathbin

clean; echo "fails=$fails"
