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

## Switch size

`switch-size.js joins.txt <phone|tablet|desktop>` (two sessions, `SHELL` as
above, 75 ms of delay): another viewer, wide and low (1000×380), leaves the
second session's PTY short with `switch-size-tui.sh` running in it (copied
to `/p`, or named by `TUI_SH`). That's a full-screen program in miniature,
drawing `BOT-<rows>` on its last row and again on every SIGWINCH. The page,
on a phone (412×915), a tall tablet (757×840, about 1968×2184 device pixels)
or a desktop, then switches to it fresh, back, and to it warm. On every
switch the session switched to is sent this screen's size (`sent`), and the
grid is never shorter than the box (`gridShortFrames` 0). On a warm switch,
the first frame that shows the program already has it on the grid's last
row. On a fresh one, the screen is asked for with the handshake, before
there's a key to send the size under, so its first frame may still be at
the old size. It's on the last row once the size lands (`fullAtMs`). Every
`ok` is true. Run it against the Go relay and against `wrangler dev` (the
sessions' `REMINAL_RELAY`/`REMINAL_WEB` pointed at it).

## Owner key, opened by PIN

`owner-pin.sh` (builds reminal in Docker, then runs `owner-pin.js` in a
Playwright container with a relay and three machines, a HOME and owner list
each) opens sessions from a browser that has an owner key: one on a machine
that does not own it, by its PIN, is live well under a second (the owner
handshake goes unanswered, and the PIN one goes a moment behind it); one on a
machine that owns it opens as an owner, with no PIN handshake; one on a
machine that stopped owning it is live by its PIN within a moment; with no
PIN, the status says within a few seconds that the PIN gets in; a browser
with no key and no PIN says "PIN required". It prints `ALL OK`.
