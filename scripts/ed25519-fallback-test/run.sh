#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A browser with no Ed25519 (Safari 16: every Ed25519 call is
# NotSupportedError), stood in for by a Chromium page whose crypto.subtle
# refuses Ed25519 so (check.mjs): its owner key made all the same and kept;
# a session on a machine it owns connected without a PIN, beside a page with
# Ed25519 of its own; signatures checked both ways; a key already kept never
# replaced; and, where tweetnacl cannot load either, a plain message. In
# Docker, against a relay in the box. SRC=<tree> builds another tree (a
# control: a checkout of main).
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
SRC=${SRC:-$ROOT}
B=$(mktemp -d); trap '"$ROOT/scripts/rm-tmp.sh" "$B"' EXIT
docker run --rm -v "$SRC":/src:ro -v "$B":/out -v reminal-gocache:/root/.cache/go-build -v reminal-gomod:/go/pkg/mod \
  -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm \
  go build -o /out/reminal ./cmd/reminal
cp "$DIR/check.mjs" "$B/"
PW=1.56.0 PWVOL=reminal-playwright-1.56.0
docker volume create "$PWVOL" >/dev/null; docker volume create reminal-npm-locks >/dev/null
docker run --rm -v "$PWVOL":/e2e/node_modules -v reminal-npm-locks:/locks -w /e2e mcr.microsoft.com/playwright:v$PW-noble sh -c '
  exec flock /locks/reminal-playwright-1.56.0.lock sh -c "[ -f node_modules/.ok ] && exit 0; npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1 && touch node_modules/.ok"' || exit 1
docker run --rm -v "$B":/b:ro -v "$PWVOL":/e2e/node_modules:ro -w /e2e -e HOME=/root -e SHELL=/bin/bash -e TERM=xterm-256color \
  -e REMINAL_RELAY=ws://127.0.0.1:8080/ws -e REMINAL_WEB=http://127.0.0.1:8080 -e REMINAL_KEYSTORE=file \
  mcr.microsoft.com/playwright:v$PW-noble sh -c 'cp /b/reminal /usr/local/bin/ && cp /b/check.mjs /e2e/t.mjs && node t.mjs'
