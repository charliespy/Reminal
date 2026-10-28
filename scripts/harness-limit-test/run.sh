#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# check.mjs: a session whose Claude Code stopped at its usage limit must be
# listed "limited", though its turn ended as any other does (its Stop hook
# says done) and its screen looks done; one that finished a turn must stay
# "done". The screens are Claude Code's own, as real sessions showed them,
# drawn by a stand-in named claude. In Docker.
#
#   run.sh [REF]   REF: build reminal from that git ref instead of this tree
#                  (a control: origin/main must fail the limit cases).
# A heavy run: waits for one of the machine's slots first (scripts/README-gates.md).
[ -n "${REMINAL_GATE_HELD:-}" ] || [ ! -f "$(dirname -- "$0")/../gatelock.py" ] || ! command -v python3 >/dev/null || exec python3 "$(dirname -- "$0")/../gatelock.py" -- sh "$0" "$@"
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
V=${REMINAL_VOLUME_PREFIX:-reminal}
B=$(mktemp -d)
IMG=reminal-harness-limit-$$
trap 'docker rmi -f "$IMG" >/dev/null 2>&1 || true; "$ROOT/scripts/rm-tmp.sh" "$B"' EXIT
SRC=$ROOT
if [ -n "${1:-}" ]; then
  mkdir "$B/src"
  git -C "$ROOT" archive "$1" | tar -x -C "$B/src"
  SRC=$B/src
fi
docker run --rm -v "$SRC":/src:ro -v "$B":/out -v "$V-gocache":/root/.cache/go-build -v "$V-gomod":/go/pkg/mod \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm \
    go build -o /out/reminal ./cmd/reminal
cp "$DIR/check.mjs" "$DIR/claude" "$B/"
cat > "$B/Dockerfile" <<'DOCKER'
FROM node:22-bookworm
COPY reminal claude /usr/local/bin/
COPY check.mjs /e2e/
ENV HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:1 REMINAL_WEB=http://127.0.0.1:1
CMD ["node", "/e2e/check.mjs"]
DOCKER
docker build -q -t "$IMG" "$B" >/dev/null
docker run --rm "$IMG"
