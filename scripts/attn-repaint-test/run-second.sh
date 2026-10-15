#!/bin/bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A second claude started in a session whose first one is suspended, and
# left at its prompt: does its repainting read as turns? The first
# (fakeidle.go) finishes a turn (its hook says "done") and is suspended
# (Ctrl-Z); the shell is in front for a while; a second starts, and repaints
# (SIGWINCH) and changes its footer with no turn. Then it takes a turn of its
# own (hook "working", then "done"). PASS = no "working" until that turn,
# and "working" then. Run in Docker, never on a real HOME:
#   docker run --rm -v "$PWD/scripts/attn-repaint-test":/t:ro -v <dir with reminal, claude>:/b:ro golang:1.26-bookworm bash /t/run-second.sh
set -u
cp /b/reminal /b/claude /usr/local/bin/; chmod +x /usr/local/bin/reminal /usr/local/bin/claude
export HOME=$(mktemp -d) REMINAL_RELAY=ws://127.0.0.1:1 REMINAL_WEB=http://127.0.0.1:1 REMINAL_KEYSTORE=file
export REMINAL_ATTENTION_PROBE=/tmp/probe.jsonl SHELL=/t/shell.sh
id=$(reminal new second 2>&1 | grep -o "[A-Z0-9]\{8\}" | head -1)
sleep 3
REMINAL_SESSION=$id reminal hook done </dev/null   # the first one's turn finished
sleep 4
first=$(pgrep -x claude | head -1)
kill -TSTP $first; sleep 12   # Ctrl-Z; the shell in front; the second starts
second=$(pgrep -x claude | grep -v "^$first$" | head -1)
[ -n "$second" ] || { echo "FAIL: no second claude"; exit 1; }
kill -WINCH $second; sleep 6  # a viewer connects: a repaint
kill -WINCH $second; sleep 6
sleep 32                      # the footer changes by itself
REMINAL_SESSION=$id reminal hook working </dev/null; sleep 4   # its own first turn
python3 - <<'PY'
import json
s=[json.loads(l) for l in open('/tmp/probe.jsonl')]
last=None
for e in s:
    k=(e['fg'],e['state'],e['source'])
    if k!=last: print(e['ts']%1000000, *k, repr(e['tail'][-40:])); last=k
turn=next(i for i,e in enumerate(s) if e['source']=='hook' and e['state']=='working')
# From the second claude in front (the first sample after the shell's) to its turn.
shell=max(i for i,e in enumerate(s[:turn]) if e['fg']!='claude')
idle=[e for e in s[shell+1:turn] if e['fg']=='claude']
bad=[e for e in idle if e['state']=='working']
if not idle: print("FAIL: the second claude was never read"); raise SystemExit(1)
if bad: print("FAIL: the idle second claude read working %d times (from %s)" % (len(bad), sorted({e['source'] for e in bad}))); raise SystemExit(1)
print("PASS: the second claude stayed at rest through its start, the repaints and the footer change, and its own turn read working")
PY
