#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The PIN gate in front of a forwarded port, on the real Worker (wrangler dev,
# local, in Docker) with a real `reminal expose`: the PIN, the lockouts, the
# visitor cookie, and a visitor from before the change keeping its way in.
#   run.sh [OLD_REF]   OLD_REF: the Worker to upgrade from (default public/main
#                      when there is such a remote, else origin/main)
set -e
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
NAME=${NAME:-reminal-expose-gate-test}
OLD=$(mktemp -d)
trap 'rm -rf "$OLD"' EXIT
REF=${1:-origin/main}
[ -n "${1:-}" ] || ! git -C "$ROOT" rev-parse -q --verify public/main >/dev/null || REF=public/main
echo "upgrading from the Worker at $REF"
git -C "$ROOT" archive "$REF" cloudflare/src | tar x -C "$OLD"
docker build -q -t "$NAME" --build-context old="$OLD" -f "$ROOT/scripts/expose-gate-test/Dockerfile" "$ROOT" >/dev/null
docker run --rm --name "$NAME" "$NAME"
