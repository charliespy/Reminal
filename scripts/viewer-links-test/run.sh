#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# scripts/viewer-links-test/run.sh — a forwarded app opened from the viewer
# opens with no way back to it (check.mjs): a relay built from this tree, a
# real Chromium, in Docker.
#
#   sh scripts/viewer-links-test/run.sh
# A heavy run: waits for one of the machine's slots first (scripts/README-gates.md).
[ -n "${REMINAL_GATE_HELD:-}" ] || [ ! -f "$(dirname -- "$0")/../gatelock.py" ] || ! command -v python3 >/dev/null || exec python3 "$(dirname -- "$0")/../gatelock.py" -- sh "$0" "$@"
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
# Docker volumes are named from REMINAL_VOLUME_PREFIX (default reminal), so a
# run can keep its caches apart from other checkouts on the machine.
V=${REMINAL_VOLUME_PREFIX:-reminal}
B=$(mktemp -d)
trap '"$DIR/../rm-tmp.sh" "$B"' EXIT
mkdir -p "$B/bin"
docker run --rm -v "$ROOT":/src:ro -v "$B":/out -v "$V-gomod":/go/pkg/mod -v "$V-gocache":/root/.cache/go-build \
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm go build -o /out/bin/reminal ./cmd/reminal
cp "$DIR/check.mjs" "$B/"
PWVOL="$V-playwright-1.56.0"
docker volume create "$PWVOL" >/dev/null; docker volume create "$V-npm-locks" >/dev/null
docker run --rm -v "$PWVOL":/e2e/node_modules -v "$V-npm-locks":/locks -w /e2e mcr.microsoft.com/playwright:v1.56.0-noble sh -c '
  exec flock /locks/reminal-playwright-1.56.0.lock sh -c "[ -f node_modules/.ok ] && exit 0; npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1 && touch node_modules/.ok"' || exit 1
docker run --rm -v "$B":/b:ro -v "$PWVOL":/e2e/node_modules:ro -w /e2e -e HOME=/root -e REMINAL_KEYSTORE=file \
  mcr.microsoft.com/playwright:v1.56.0-noble sh -c 'cp /b/bin/reminal /usr/local/bin/ && cp /b/check.mjs /e2e/ && node /e2e/check.mjs'
