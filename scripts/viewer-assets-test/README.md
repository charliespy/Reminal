# The viewer's scripts as cached assets, under a CSP with no 'unsafe-inline'

In Docker, against a relay serving this tree (`reminal relay 8080`, or the
Worker under `wrangler dev`), the viewer at `http://localhost:<port>` from a
Playwright container in that relay's network:

- `csp.js joins.txt` loads the page on a desktop and a phone, connects, opens
  Share, settings, the key bar, find, Recents, the host panel, the Machines
  panel and the session dock, and types. Expect `violations` and `consoleCSP`
  empty on both, both scripts from `/assets/` with
  `Cache-Control: public, max-age=31536000, immutable`, `typed` true, and a
  CSP with no `'unsafe-inline'`.
- `opens.js joins.txt` opens the page three times in one browser (a Pixel 7,
  CPU 4x), its cache kept; with `tc qdisc replace dev lo root netem delay
  75ms rate 1600kbit` on the relay's container, the phone profile. Expect the
  second and third opens well under the first, and `swCaches` 0 (the service
  worker caches nothing).
