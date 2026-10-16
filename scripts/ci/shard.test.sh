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
T=$(mktemp -d); trap 'rm -rf "${T:?}"' EXIT
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
exit $fail
