#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# mcp.sh send <id> <keys> | type <id> <keys> (no Enter) | read <id> — one reminal MCP call, the
# JSON built by node so any text (quotes, backslashes) is typed as given.
# mcp.sh text <id> — what read_transcript says the screen holds, as text,
# read out of its answer here in the box (by node, never on the host).
if [ "$1" = text ]; then
	"$0" read "$2" | node -e '
let s = "";
process.stdin.on("data", (d) => (s += d)).on("end", () => {
  try {
    for (const c of JSON.parse(s).result?.content ?? []) {
      if ((c.text ?? "").startsWith("{")) console.log(JSON.parse(c.text).text ?? "");
    }
  } catch {}
});'
	exit 0
fi
op=$1 id=$2 keys=$3
call=$(OP="$op" ID="$id" KEYS="$keys" node -e '
const {OP, ID, KEYS} = process.env;
const typing = OP === "send" || OP === "type";
const args = typing ? {session: ID, keys: KEYS, enter: OP === "send"} : {session: ID};
console.log(JSON.stringify({jsonrpc: "2.0", id: 2, method: "tools/call",
  params: {name: typing ? "send_keys" : "read_transcript", arguments: args}}));')
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}'
( printf '%s\n%s\n' "$init" "$call"; sleep 2 ) | reminal mcp 2>/dev/null | tail -1
