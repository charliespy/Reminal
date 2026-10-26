#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# scripts/viewer-csp-test/run.sh — the viewer under the content policy it is
# served with (check.mjs): a relay built from this tree, a machine this
# browser owns, a real Chromium. What a person does on the page — connect
# and type, Share and its QR code, preview an image and a PDF, download a
# file, the app list with its icons, alerts' service worker, the tour's
# fonts — is refused nothing, and an app icon a machine names on another
# host is not fetched (the list shows its letter instead).
#
#   sh scripts/viewer-csp-test/run.sh
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
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm sh -c '
  set -e
  go build -o /out/bin/reminal-relay ./cmd/reminal
  go build -o /out/bin/reminal ./cmd/reminal'
cp "$DIR/check.mjs" "$B/"
PWVOL="$V-playwright-1.56.0"
docker volume create "$PWVOL" >/dev/null; docker volume create "$V-npm-locks" >/dev/null
docker run --rm -v "$PWVOL":/e2e/node_modules -v "$V-npm-locks":/locks -w /e2e mcr.microsoft.com/playwright:v1.56.0-noble sh -c '
  exec flock /locks/reminal-playwright-1.56.0.lock sh -c "[ -f node_modules/.ok ] && exit 0; npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1 && touch node_modules/.ok"' || exit 1
docker run --rm -v "$B":/b:ro -v "$PWVOL":/e2e/node_modules:ro -w /e2e -e HOME=/root -e SHELL=/bin/bash -e TERM=xterm-256color \
  -e REMINAL_RELAY=ws://127.0.0.1:8080/ws -e REMINAL_WEB=http://127.0.0.1:8080 -e REMINAL_KEYSTORE=file -e REMINAL_HOSTNAME=cspbox \
  mcr.microsoft.com/playwright:v1.56.0-noble sh -c 'cp /b/bin/reminal /b/bin/reminal-relay /usr/local/bin/ && cp /b/check.mjs /e2e/ && node /e2e/check.mjs'
