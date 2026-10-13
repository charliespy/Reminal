# Revoking an owner device signs it out of open sessions

`revoke.js` drives the real viewer through it, in Docker (never on this
machine):

1. A container runs `reminal relay 8080` and one session started with
   `SHELL=ticker-shell.sh` (a background ticker writes `/tmp/tick-<pid>` every
   second — the program that must survive). Its Join link goes in `joins.txt`.
2. A loop beside it answers the script through files in `/p`: `enroll-<tag>`
   → `reminal add owner <id> -y`, then `enrolled-<tag>`; `revoke` (one or more
   ids) → `reminal owners revoke <ids>`, then `revoked` / `revoked2`; it writes
   the tick count and whether the ticker lives to `/p/ticks`.
3. `mcr.microsoft.com/playwright` in that container's network, the viewer at
   `http://localhost:8080`: `node revoke.js joins.txt`.

Every field must come out as: `revokedLiveCut` true and `revokedState.key`
false (the revoked device's open viewer has no key), `pinViewerReconnected` 1,
`enrolledOwnerBack` true, `revokedFreshDial.key` false, `twoRevokesRestarts` 1
with `twoCut` both true, `selfRevokeOK` true, `selfRevokedState.key` false,
`selfRevokeRestarts` 1 — and the ticker alive with its count still rising.
