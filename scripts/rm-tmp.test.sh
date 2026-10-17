#!/bin/sh
# rm-tmp.sh removes only what lies under a temp root, and says what it
# refuses. Run: sh scripts/rm-tmp.test.sh (CI, shard 1).
set -u
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
R=$DIR/rm-tmp.sh fail=0
ok() { if eval "$2"; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi; }
T=$(mktemp -d)
K=$(mktemp -d "${HOME:-/root}/.rm-tmp-test.XXXXXX") # under a home, not a temp root
mkdir -p "$T/a/b" "$K/x"
ok 'removes a dir mktemp made' '"$R" "$T/a" && [ ! -e "$T/a" ]'
ok 'skips an empty argument' '"$R" ""'
ok 'refuses a dir outside every temp root' '! "$R" "$K/x" 2>/dev/null && [ -d "$K/x" ]'
ok 'refuses a path with ..' '! "$R" "$T/../../$K" 2>/dev/null && [ -d "$K" ]'
ok 'refuses a temp root itself' '! "$R" /tmp 2>/dev/null && ! "$R" /tmp/ 2>/dev/null && [ -d /tmp ]'
ok 'refuses / and an empty TMPDIR does not widen it' '! TMPDIR= RUNNER_TEMP=/ "$R" / "$K" 2>/dev/null && [ -d "$K" ]'
ok 'says why it refused' '"$R" "$K" 2>&1 | grep -q "not under a temp dir"'
ok 'a TMPDIR of $HOME does not make the home a temp root' '! TMPDIR=$HOME "$R" "$K/x" 2>/dev/null && [ -d "$K/x" ]'
ok 'nor does a TMPDIR above $HOME' '! TMPDIR=$(dirname "$HOME") "$R" "$K/x" 2>/dev/null && [ -d "$K/x" ]'
ln -s "$K" "$T/link"
ok 'refuses a path that a link in /tmp leads out of it' '! "$R" "$T/link/x" 2>/dev/null && [ -d "$K/x" ]'
ok 'removes a link in a temp dir, not what it points at' '"$R" "$T/link" && [ ! -L "$T/link" ] && [ -d "$K/x" ]'
D=$(mktemp -d "$K/deep.XXXXXX"); mkdir -p "$D/a/b/c"
ok 'a deep TMPDIR inside the home still serves as a temp root' 'TMPDIR=$D/a/b "$R" "$D/a/b/c" && [ ! -e "$D/a/b/c" ]'
rm -rf -- "$K"
"$R" "$T"; ok 'removes the test dir' '[ ! -e "$T" ]'
[ "$fail" = 0 ] && echo "all passed" || exit 1
