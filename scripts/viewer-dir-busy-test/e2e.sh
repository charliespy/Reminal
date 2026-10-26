#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# e2e.mjs: two browsers hammering one machine past its allowance — a relay
# and a machine built from this tree, and the page they serve, two owners
# of the machine. Every request must be answered (busy, then taken), none
# read as the machine being offline, every session asked for started.
# A heavy run: waits for one of the machine's slots first (scripts/README-gates.md).
[ -n "${REMINAL_GATE_HELD:-}" ] || [ ! -f "$(dirname -- "$0")/../gatelock.py" ] || ! command -v python3 >/dev/null || exec python3 "$(dirname -- "$0")/../gatelock.py" -- sh "$0" "$@"
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
# Docker volumes are named from REMINAL_VOLUME_PREFIX (default reminal), so a
# run can keep its caches apart from other checkouts on the machine.
V=${REMINAL_VOLUME_PREFIX:-reminal}
B=$(mktemp -d)
IMG=reminal-dir-busy-e2e-$$
trap 'docker rmi -f "$IMG" >/dev/null 2>&1 || true; "$ROOT/scripts/rm-tmp.sh" "$B"' EXIT
docker run --rm -v "$ROOT":/src:ro -v "$B":/out -v "$V-away-gocache":/root/.cache/go-build -v "$V-away-gomod":/go/pkg/mod \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm \
    go build -o /out/reminal ./cmd/reminal
cp "$DIR/e2e.mjs" "$B/"
cat > "$B/Dockerfile" <<'DOCKER'
FROM mcr.microsoft.com/playwright:v1.56.0-noble
WORKDIR /e2e
RUN npm i --silent playwright@1.56.0 >/dev/null 2>&1
COPY reminal /usr/local/bin/
COPY e2e.mjs /e2e/
ENV HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:8080/ws REMINAL_WEB=http://127.0.0.1:8080
CMD ["node", "/e2e/e2e.mjs"]
DOCKER
docker build -q -t "$IMG" "$B" >/dev/null
docker run --rm "$IMG"
