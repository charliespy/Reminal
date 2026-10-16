#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# shard.sh's promise, checked on this tree for the shard count CI uses: every
# test internal/client has runs in exactly one shard — none lost, none twice
# — and the shards are within a few seconds of each other.
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
T=$(mktemp -d); trap 'rm -rf "${T:?}"' EXIT
fail=0
go test -list '.*' ./internal/client | grep -E '^(Test|Example|Fuzz)[A-Za-z0-9_]*$' > "$T/all"
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
exit $fail
