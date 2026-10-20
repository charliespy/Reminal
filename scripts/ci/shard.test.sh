#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# shard.sh's promise, checked on this tree for the shard count CI uses: every
# test internal/client has, plainly or under -race, runs in exactly one shard
# — none lost, none twice — and the shards are within a few seconds of each
# other. And on a package of its own making, the case this tree may not have
# yet: a test file under a race build constraint is in a shard all the same.
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
T=$(mktemp -d); trap '"$DIR/../rm-tmp.sh" "$T"' EXIT
fail=0
{ go test -list '.*' ./internal/client; go test -race -list '.*' ./internal/client; } | grep -E '^(Test|Example|Fuzz)[A-Za-z0-9_]*$' | sort -u > "$T/all"
for N in 4; do
  : > "$T/hits"
  for K in $(seq 1 "$N"); do
    sh "$DIR/shard.sh" "$N" "$K" > "$T/re.$K"
    grep -Ex "$(cat "$T/re.$K")" "$T/all" >> "$T/hits" || true
  done
  lost=$(sort "$T/all" | comm -23 - "$(sort "$T/hits" > "$T/h.s"; echo "$T/h.s")" | wc -l | tr -d ' ')
  twice=$(sort "$T/hits" | uniq -d | wc -l | tr -d ' ')
  loads=$(sh "$DIR/shard.sh" --plan "$N" | sed -n 's/^# shard [0-9]*: //p' | tr -d ' s' | tr '\n' ' ')
  spread=$(echo "$loads" | awk '{mn=$1; mx=$1; for(i=2;i<=NF;i++){if($i<mn)mn=$i; if($i>mx)mx=$i} printf "%.1f", mx-mn}')
  if [ "$lost" != 0 ] || [ "$twice" != 0 ] || awk -v s="$spread" 'BEGIN{exit !(s > 15)}'; then
    echo "FAIL N=$N: $lost lost, $twice twice, shard times $loads(spread ${spread}s)"; fail=1
  else
    echo "ok   N=$N: $(wc -l < "$T/all" | tr -d ' ') tests, each once; shard times ${loads}"
  fi
done

# A test only a -race build has (//go:build race), one only a plain build
# has (!race), and one both have: each in exactly one of two shards.
M="$T/mod"; mkdir -p "$M"
printf 'module shardfixture\n\ngo 1.21\n' > "$M/go.mod"
# No times: each test counts as the median. (Not empty: awk takes an empty
# first file's place for the next one's lines.)
echo '# no times' > "$T/times"
printf 'package shardfixture\n\nimport "testing"\n\nfunc TestBoth(t *testing.T) {}\n' > "$M/both_test.go"
printf '//go:build race\n\npackage shardfixture\n\nimport "testing"\n\nfunc TestRaceOnly(t *testing.T) {}\n' > "$M/race_test.go"
printf '//go:build !race\n\npackage shardfixture\n\nimport "testing"\n\nfunc TestPlainOnly(t *testing.T) {}\n' > "$M/norace_test.go"
: > "$T/fhits"
for K in 1 2; do
  re=$(cd "$M" && GOWORK=off SHARD_PKG=. SHARD_TIMES="$T/times" sh "$DIR/shard.sh" 2 "$K")
  printf 'TestBoth\nTestRaceOnly\nTestPlainOnly\n' | grep -Ex "$re" >> "$T/fhits" || true
done
got=$(sort "$T/fhits" | tr '\n' ' ')
if [ "$got" = "TestBoth TestPlainOnly TestRaceOnly " ]; then
  echo "ok   race build constraints: a race-only and a plain-only test each in one shard"
else
  echo "FAIL race build constraints: the shards hold [$got], want each of TestBoth TestPlainOnly TestRaceOnly once"; fail=1
fi
# Tests of no known time (all new, say), and tests of no weight: spread
# over the shards, never all in one (a weight of 0 never made a shard less
# loaded than another, so all of them went to the first chosen).
F="$T/spread"; mkdir -p "$F"
printf 'module shardspread\n\ngo 1.21\n' > "$F/go.mod"
{ printf 'package shardspread\n\nimport "testing"\n\n'; for i in $(seq 1 40); do printf 'func TestNew%02d(t *testing.T) {}\nfunc TestZero%02d(t *testing.T) {}\n' "$i" "$i"; done; } > "$F/s_test.go"
{ echo '# zero-weight tests'; for i in $(seq 1 40); do printf 'TestZero%02d\t0.00\n' "$i"; done; } > "$T/ztimes"
counts=""
for K in 1 2 3 4; do
  re=$(cd "$F" && GOWORK=off SHARD_PKG=. SHARD_TIMES="$T/ztimes" sh "$DIR/shard.sh" 4 "$K")
  counts="$counts $( (for i in $(seq 1 40); do printf 'TestNew%02d\nTestZero%02d\n' "$i" "$i"; done) | grep -Ecx "$re" || true)"
done
if echo "$counts" | awk '{mn=$1; mx=$1; for(i=2;i<=NF;i++){if($i<mn)mn=$i; if($i>mx)mx=$i}; exit !(mx-mn <= 1)}'; then
  echo "ok   tests of no known time or no weight spread over the shards:$counts"
else
  echo "FAIL tests of no known time or no weight piled up: shards hold$counts of 80"; fail=1
fi
# The same with weights mixed (the real file's shape): slow tests close
# in time, as a balanced split leaves them, and many of 0.00 — the zeros
# spread too, each shard's count within 2× of another's (a zero once added
# nothing, so every one of them went to the shard least loaded when they
# began, however little less).
{ echo '# mixed'; printf 'TestSlow1\t2.03\nTestSlow2\t2.02\nTestSlow3\t2.01\nTestSlow4\t2.00\n'; for i in $(seq 1 40); do printf 'TestZero%02d\t0.00\n' "$i"; done; } > "$T/mtimes"
printf 'func TestSlow1(t *testing.T) {}\nfunc TestSlow2(t *testing.T) {}\nfunc TestSlow3(t *testing.T) {}\nfunc TestSlow4(t *testing.T) {}\n' >> "$F/s_test.go"
mcounts=""
for K in 1 2 3 4; do
  re=$(cd "$F" && GOWORK=off SHARD_PKG=. SHARD_TIMES="$T/mtimes" sh "$DIR/shard.sh" 4 "$K")
  mcounts="$mcounts $( (for i in $(seq 1 40); do printf 'TestZero%02d\n' "$i"; done) | grep -Ecx "$re" || true)"
done
if echo "$mcounts" | awk '{mn=$1; mx=$1; for(i=2;i<=NF;i++){if($i<mn)mn=$i; if($i>mx)mx=$i}; exit !(mn > 0 && mx <= 2*mn)}'; then
  echo "ok   with slow tests beside them, the tests of 0.00 spread too:$mcounts"
else
  echo "FAIL with slow tests beside them, the tests of 0.00 piled up: shards hold$mcounts of 40"; fail=1
fi
exit $fail
