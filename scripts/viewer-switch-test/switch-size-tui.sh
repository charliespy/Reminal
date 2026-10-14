#!/bin/bash
# A full-screen program in miniature: draws its last row, again on every SIGWINCH.
draw() { L=$(tput lines); printf '\033[H\033[2J\033[%d;1HBOT-%s' "$L" "$L"; }
trap draw WINCH
draw
while :; do sleep 1 & wait $!; done
