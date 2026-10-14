#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A program that draws as an agent's interface does: a block of rows drawn
# once, then each tick only the cells that changed, reached by relative
# moves from where it left the cursor (below the block), never by
# addressing a row. Started by Enter; 60 ticks, then still.
printf '\033[?25l'
for l in 1 2 3 4 5 6; do echo "row $l: ______________________________"; done
read _
printf '\033[1A'
i=0
while [ $i -lt 60 ]; do
  i=$((i + 1)); c=$(( (i * 7) % 30 ))
  ch=$(printf "\\$(printf '%03o' $((97 + i % 26)))")
  printf '\033[6A'
  for l in 1 2 3 4 5 6; do printf '\r\033[%dC%s\033[1B' $((8 + c)) "$ch"; done
  printf '\r'
  sleep 0.08
done
printf '\033[2K\rTUI-DONE\n'
exec sleep 100000
