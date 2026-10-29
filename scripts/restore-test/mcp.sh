#!/bin/sh
# mcp.sh send <id> <keys> | mcp.sh type <id> <keys> | mcp.sh read <id> | mcp.sh sendpin <id> <pin> <keys>
# | mcp.sh list | mcp.sh search <pattern>   (MCP_WAIT=<s>: how long to wait
# for the answer, for a call that takes long, as send_keys waking a session)
# (sendpin types as a person at a viewer would: through the relay, with the
# session's PIN — what the session counts as someone typing)
init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}
{"jsonrpc":"2.0","method":"notifications/initialized"}'
if [ "$1" = send ] || [ "$1" = type ]; then
  enter=true; [ "$1" = type ] && enter=false   # type: the keys, no Return
  call=$(printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_keys","arguments":{"session":"%s","keys":"%s","enter":%s}}}' "$2" "$3" "$enter")
elif [ "$1" = sendpin ]; then
  call=$(printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send_keys","arguments":{"session":"%s","pin":"%s","keys":"%s","enter":true}}}' "$2" "$3" "$4")
elif [ "$1" = list ]; then
  call='{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}'
elif [ "$1" = search ]; then
  call=$(printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"search_sessions","arguments":{"pattern":"%s"}}}' "$2")
else
  call=$(printf '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_transcript","arguments":{"session":"%s"}}}' "$2")
fi
( printf '%s\n%s\n' "$init" "$call"; sleep ${MCP_WAIT:-2} ) | reminal mcp 2>/dev/null | tail -1
