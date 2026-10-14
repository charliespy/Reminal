# Window mirror over WebRTC, end to end

`mirror.js` opens a window pane in the real viewer and checks that frames
arrive over the peer-to-peer DataChannel and that typing reaches the window
over the input channel. In Docker, on the X11 test image
(`scripts/x11-test`, which brings up Xvfb, openbox and an xterm running
`sh -i`):

1. `docker run -d` the image with `sleep infinity`, `HOME=/root/h`,
   `REMINAL_RELAY=ws://localhost:8080/ws`, `REMINAL_WEB=http://localhost:8080`;
   inside, `reminal relay 8080` and `reminal new` — its Join link in `joins.txt`.
2. `mcr.microsoft.com/playwright` in that container's network:
   `node mirror.js joins.txt <secs>` — or `… <secs> nopane` to hold a viewer
   with its peer connection up and no pane, for measuring an idle agent.
3. Expect `windowFound`, `frames`, `p2p` and `inputChannelOpen` all true, and
   the xterm to have run what was typed: `/tmp/typed` reads `MIRROR-OK`.
