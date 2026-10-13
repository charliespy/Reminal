# A viewer socket that dies without a close

`dead-socket.js` reproduces a network change (wifi to cell): the viewer's
open connection to the relay stops carrying anything, and no close ever
comes. In Docker, never on this machine:

1. A container (`--cap-add NET_ADMIN`, iptables) runs `reminal relay 8080`
   and one session, its Join link in `joins.txt`.
2. When the script creates `/p/drop`, a loop in that container drops the
   browser's current flows to :8080 — the client sockets `ss` shows with no
   process (the browser is another container's) — with iptables AND
   ip6tables (localhost is ::1 to Chrome), both directions; then `/p/dropped`.
   New connections still work: only the old flows are dead.
3. `mcr.microsoft.com/playwright` in that container's network, the viewer at
   `http://localhost:8080`: `node dead-socket.js joins.txt`.

Expect `keysAccountedFor` true (the notice counting what may not have
arrived), `redialled` true within a few seconds, `keysAfterRedialArrive` true,
and `onlineRedialled` true after the `online` event. On a viewer without the
probe the keys vanish without a word and the page never redials.
