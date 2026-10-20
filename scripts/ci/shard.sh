#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# internal/client's tests split into N shards of about equal time, for CI to
# run side by side (release.yml: test, plainly and under -race). Every top-level test the
# package has now (go test -list, with the tags given) lands in exactly one
# shard: assigned greedily, slowest first, each to the shard with the least
# time so far (of two as loaded, the one with fewer tests), by its time in
# client-test-times.tsv; a test not in it (new) counts as their mean.
# Deterministic: the same tree splits the same way.
#
#   scripts/ci/shard.sh N K [TAGS]   the -run pattern for shard K (1..N)
#   scripts/ci/shard.sh --plan N [TAGS]   every test's shard, and each
#                                         shard's time (to check by eye)
#
# SHARD1_HEAD=<seconds>: time shard 1 has already (the rest of the suite it
# also runs), so it gets fewer of these.
#
# The times: regenerate after the suite changes a lot —
#   go test -race -json ./internal/client > /tmp/c.json
#   python3 -c 'import json,sys
#   t={e["Test"]:e.get("Elapsed",0) for l in open(sys.argv[1]) for e in [json.loads(l)]
#      if e.get("Action") in ("pass","fail","skip") and e.get("Test") and "/" not in e["Test"]}
#   [print(f"{k}\t{v:.2f}") for k,v in sorted(t.items())]' /tmp/c.json
# under the header lines of scripts/ci/client-test-times.tsv.
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
plan=false
if [ "${1:-}" = --plan ]; then plan=true; shift; fi
N=${1:?N shards}
if [ "$plan" = true ]; then K=0; TAGS=${2:-}; else K=${2:?shard K of N}; TAGS=${3:-}; fi
case "$N$K" in *[!0-9]*) echo "shard.sh: N and K are numbers" >&2; exit 2 ;; esac
[ "$N" -ge 1 ] || { echo "shard.sh: N is at least 1" >&2; exit 2; }
[ "$plan" = true ] || [ "$K" -ge 1 -a "$K" -le "$N" ] || { echo "shard.sh: K is 1..$N" >&2; exit 2; }

# The package (SHARD_PKG) and the times (SHARD_TIMES): internal/client and
# client-test-times.tsv, unless shard.test.sh's own check names others.
PKG=${SHARD_PKG:-./internal/client}
TIMES=${SHARD_TIMES:-$DIR/client-test-times.tsv}

# What the package has now, as this build sees it (its tags), plainly and
# under -race: the shards run both ways, and a test file under a race build
# constraint (//go:build race, or !race) is in only one of the two lists.
# Every test of either is in a shard; each run runs those its build has.
# A list that cannot be made fails the shard, never shrinks it.
lists=$(go test -tags "$TAGS" -list '.*' "$PKG") || { echo "shard.sh: go test -list failed for $PKG" >&2; exit 1; }
rlists=$(go test -race -tags "$TAGS" -list '.*' "$PKG") || { echo "shard.sh: go test -race -list failed for $PKG" >&2; exit 1; }
list=$(printf '%s\n%s\n' "$lists" "$rlists" | grep -E '^(Test|Example|Fuzz)[A-Za-z0-9_]*$' | sort -u || true)
[ -n "$list" ] || { echo "shard.sh: no tests listed in $PKG" >&2; exit 1; }

# Each test with its weight (unknown: the mean), slowest first. The mean,
# not the median: most tests take no time to the hundredth, so the median
# is 0, and a test of no weight never makes a shard less loaded than
# another — every new test went to one shard, the slowest ones too (one
# shard at 6 min, the others at 1).
printf '%s\n' "$list" | awk -F'\t' '
  # A time of 0.00 is under the hundredth the file keeps, not nothing: half
  # of it (0.005 s), so no test weighs 0 — one that did never changed which
  # shard was least loaded, and every one of them went to the same shard.
  NR == FNR { if ($0 !~ /^#/ && NF == 2) { t = $2 + 0; if (t < 0.005) t = 0.005; w[$1] = t; sum += t; m++ } ; next }
  { name[++n] = $0 }
  END {
    mean = m ? sum / m : 0.5
    for (i = 1; i <= n; i++) printf "%.3f\t%s\n", (name[i] in w) ? w[name[i]] : mean, name[i]
  }' "$TIMES" - |
  sort -t "$(printf '\t')" -k1,1nr -k2,2 |
  awk -F'\t' -v N="$N" -v K="$K" -v head="${SHARD1_HEAD:-0}" -v plan="$plan" '
    BEGIN { for (s = 1; s <= N; s++) { load[s] = 0; tests[s] = 0 }; load[1] = head + 0 }
    {
      # The least loaded; of two as loaded, the one with fewer tests: tests
      # of no weight are spread, not all given the first.
      best = 1
      for (s = 2; s <= N; s++) if (load[s] < load[best] || (load[s] == load[best] && tests[s] < tests[best])) best = s
      load[best] += $1
      tests[best]++
      if (plan == "true") print best "\t" $1 "\t" $2
      else if (best == K) pick[++p] = $2
    }
    END {
      if (plan == "true") { for (s = 1; s <= N; s++) printf "# shard %d: %.1f s\n", s, load[s]; exit }
      if (!p) { print "^$"; exit }
      out = "^(" pick[1]
      for (i = 2; i <= p; i++) out = out "|" pick[i]
      print out ")$"
    }'
