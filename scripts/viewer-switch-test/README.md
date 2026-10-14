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
