#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# What a restored session types is text, whatever its agent was started with
# (run.sh hostile): a claude started with a flag whose value holds control
# characters — the end of a bracketed paste, then ^U to wipe the line, then a
# command — comes back resumed without that flag, a line saying it was left
# out, and nothing else run. A terminal acts on control characters before any
# shell sees a quote, so quoting alone cannot keep them in.
BOX=${BOX:-reminal-restore-box}
fails=0
ok()   { echo "ok   $*"; }
fail() { echo "FAIL $*"; fails=$((fails+1)); }
box()  { docker exec $BOX sh -c "$1"; }
send() { box "mcp.sh send $1 '$2' >/dev/null"; }
screen() { box "mcp.sh read $1"; }
idof()  { echo "$1" | sed -n 's/^ *Session: *\([A-Z0-9]*\).*/\1/p' | head -1; }
# The last start of claude in argv.log. reminal also runs it to ask its
# --version or --help, or to add its MCP server; those start no session.
laststart() { box "grep -v -x -e 'argv: --version' -e 'argv: -v' -e 'argv: --help' -e 'argv: mcp .*' /root/.fake-claude/argv.log 2>/dev/null | tail -1"; }

# Started from a script, so the agent's own command line holds the bytes.
box 'mkdir -p /root/w && cat > /root/w/start.sh <<"E"
#!/bin/sh
exec claude --append-system-prompt "$(printf "\033[201~\025touch /root/typed-from-an-argument #")"
E
chmod +x /root/w/start.sh; rm -f /root/typed-from-an-argument'
ID=$(idof "$(box 'cd /root/w && reminal new hostile 2>&1')")
[ -n "$ID" ] && ok "session $ID" || { fail "no session"; exit 1; }
send $ID '/root/w/start.sh'
sleep 3
send $ID 'hello'
sleep 18   # past a save (every 15s)
box 'pgrep -x claude >/dev/null' && ok "claude runs, its argument holding control characters" || fail "claude not running"

docker restart $BOX >/dev/null
ok "rebooted"
for i in $(seq 1 40); do laststart | grep -q -- --resume && break; sleep 1; done
sleep 4

box 'test -e /root/typed-from-an-argument' && fail "the restore typed a command out of the agent's argument" || ok "nothing in the agent's argument was run"
laststart | grep -q -- "--resume" && ok "claude resumed: $(laststart | cat -v)" || fail "claude not resumed: $(box 'tail -2 /root/.fake-claude/argv.log' | cat -v)"
laststart | grep -q "$(printf '\033')" && fail "the resume still carries the control characters" || ok "the resume carries none of them"
screen $ID | grep -q "left out --append-system-prompt" && ok "a line says the flag was left out" || fail "no line saying what was left out"

[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
