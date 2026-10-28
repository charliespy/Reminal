#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# e2e.mjs: sleep and wake from the viewer — a relay and a machine built from
# this tree, the page it serves as an owner device of the machine, and a
# stand-in claude. Nothing on the host is touched; all of it runs in one
# container.
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
V=${REMINAL_VOLUME_PREFIX:-reminal}
B=$(mktemp -d)
IMG=reminal-viewer-sleep-e2e-$$
trap 'docker rmi -f "$IMG" >/dev/null 2>&1 || true; "$ROOT/scripts/rm-tmp.sh" "$B"' EXIT
docker run --rm -v "$ROOT":/src:ro -v "$B":/out -v "$V-away-gocache":/root/.cache/go-build -v "$V-away-gomod":/go/pkg/mod \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm \
    sh -c 'go build -o /out/reminal ./cmd/reminal && go build -o /out/claude ./scripts/restore-test/fakeclaude'
cp "$DIR/e2e.mjs" "$B/"
cat > "$B/Dockerfile" <<'DOCKER'
FROM mcr.microsoft.com/playwright:v1.56.0-noble
WORKDIR /e2e
RUN npm i --silent playwright@1.56.0 >/dev/null 2>&1
COPY reminal claude /usr/local/bin/
COPY e2e.mjs /e2e/
ENV HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:8080/ws REMINAL_WEB=http://127.0.0.1:8080
CMD ["node", "/e2e/e2e.mjs"]
DOCKER
docker build -q -t "$IMG" "$B" >/dev/null
docker run --rm "$IMG"
