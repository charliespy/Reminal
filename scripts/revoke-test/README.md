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

The loop also writes the session's PIN (`reminal info`) to `/p/pins` each
round. Every field must come out as:
- `ownerLearnedPIN` true: a device's Share menu could fetch the PIN;
- `ownerStoredNoPIN` true: a device that only ever connected as an owner keeps no PIN;
- `revokedLiveCut` true and `revokedState.key` false: the revoked device's open viewer has no key;
- `revokedSharePin` "": mid-cut, it asks for the PIN as Share would and gets nothing;
- `oldPinRefused` true, `newPinPrinted` true, `newPinWorks` true: the PIN changed with the key;
- `enrolledOwnerBack` true: an owner still enrolled reconnects with no PIN;
- `revokedFreshDial.key` false, and `revokedPinDial` "PIN mismatch": the revoked device gets in neither as an owner nor with the PIN it learned;
- `twoRevokesRestarts` 1, with `twoCut` both true: two devices in one command, one restart;
- `selfRevokeOK` true, `selfRevokedState.key` false, `selfRevokeRestarts` 1;
- and the ticker alive, its count still rising.
