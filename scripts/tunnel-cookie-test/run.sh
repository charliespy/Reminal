#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A tunnel's cookies and the requests other tunnels' pages make to it, on the
# real Worker (wrangler dev, local, in Docker) with a real `reminal expose`.
# Every port-<id>.reminal.app is the same site, so one tunnel's app must not
# set cookies for the others, nor ride the visitor's gate cookie into them.
set -e
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
NAME=${NAME:-reminal-tunnel-cookie-test}
docker build -q -t "$NAME" -f "$ROOT/scripts/tunnel-cookie-test/Dockerfile" "$ROOT" >/dev/null
docker run --rm --name "$NAME" "$NAME"
