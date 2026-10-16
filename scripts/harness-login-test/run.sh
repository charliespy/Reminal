#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# check.mjs: each agent CLI reminal sets up, started with no login (or one it
# can no longer use) in a real reminal session, must be listed "logged out"
# by that session — and one that needs no login must not. Real CLIs, at the
# versions pinned here (the screens in internal/client/testdata/harness-
# screens were captured from them), in Docker. Cursor's CLI comes from its
# install script, which installs its current build only.
# A heavy run: waits for one of the machine's slots first (scripts/README-gates.md).
[ -n "${REMINAL_GATE_HELD:-}" ] || ! command -v python3 >/dev/null || [ ! -f "$(dirname -- "$0")/../gatelock.py" ] || exec python3 "$(dirname -- "$0")/../gatelock.py" -- sh "$0" "$@"
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
B=$(mktemp -d)
IMG=reminal-harness-login-$$
trap 'docker rmi -f "$IMG" >/dev/null 2>&1 || true; rm -rf "${B:?}"' EXIT
docker run --rm -v "$ROOT":/src:ro -v "$B":/out -v reminal-away-gocache:/root/.cache/go-build -v reminal-away-gomod:/go/pkg/mod \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src golang:1.26-bookworm \
    go build -o /out/reminal ./cmd/reminal
cp "$DIR/check.mjs" "$B/"
cat > "$B/Dockerfile" <<'DOCKER'
FROM node:22-bookworm
RUN npm install -g --silent @openai/codex@0.162.1 @google/gemini-cli@0.62.0 opencode-ai@1.18.34 @qwen-code/qwen-code@0.24.7 >/dev/null 2>&1
RUN mkdir -p /opt/c && HOME=/opt/c sh -c 'curl -fsSL https://cursor.com/install | bash' >/dev/null 2>&1 && ln -sf /opt/c/.local/bin/cursor-agent /usr/local/bin/cursor-agent
COPY reminal /usr/local/bin/
COPY check.mjs /e2e/
ENV HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:1 REMINAL_WEB=http://127.0.0.1:1
CMD ["node", "/e2e/check.mjs"]
DOCKER
docker build -q -t "$IMG" "$B" >/dev/null
docker run --rm -e ONLY "$IMG"
