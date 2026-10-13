#!/bin/bash
echo PIPE-OK-$$
( while :; do date +%s >> /tmp/tick-$PPID; sleep 1; done ) &
echo $! > /tmp/ticker-$$
exec bash -i
