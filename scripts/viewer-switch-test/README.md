# Viewer switch: the remembered screen

`cached-screen.js` drives the real viewer in Playwright through every case in
which a remembered screen must be shown or taken down: a switch back (shown at
once, then live, and no "Disconnected" line carried over), a wrong PIN, a
forgotten session, an ended session, and pagehide. Run it in Docker, never on
this machine:

1. A container running `reminal relay 8080` and four sessions (`SHELL` a
   script that echoes `PIPE-OK-$$` and then runs `bash -i`), their Join links
   in `joins.txt`, and 75 ms of `tc netem` delay on `lo` so a switch takes
   long enough to see the remembered screen.
2. The session third in `joins.txt` is ended (`reminal kill -y`) when the
   script creates `/p/kill-<id>`: a loop in that container waiting for it.
3. `mcr.microsoft.com/playwright` in that container's network
   (`--network container:<name>`), the viewer at `http://localhost:8080`:
   `node cached-screen.js joins.txt`. Every field of the JSON it prints is
   true, and `afterPagehide` is 0.

## Warm sockets

`warm-sockets.js joins.txt` (five sessions) tours them, then switches among the
last four: every switch must be `park,unpark,resume` on the wire — no `auth`, no
handshake — and live; typing works after one; a session whose agent restarts
(`reminal restart`, asked of `rig-ctl.py` — `python3 rig-ctl.py <reminal>`
in the agents' container; the browser shares its network, so it is reached
at 127.0.0.1:18999, which unlike a file in a shared mount is seen at once)
loses its parked socket and the switch back dials afresh; pagehide closes
them all.

`warm-revoke.js joins.txt` (two sessions, one owner device): with one
session parked and one live, the device is revoked (`reminal owners revoke`,
on `/p/revoke`): the parked socket is closed, the live one cut, and the
switch back dials afresh and gets no key.

`warm-idle.js joins.txt <secs>` holds one live and three parked sockets for
`<secs>`, then one live alone for as long, writing the phase to `/p/phase` so
the relay's and the agents' load can be sampled in each. Against the Worker,
count the Durable Object's websocket events with a `console.log` in
`webSocketMessage` in a scratch copy (never in the tree).
