#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A page whose own script did not load — the relay upgraded between serving
# the page and its scripts, so the build it names is gone — reloads once and
# comes up on the build the relay has now; one whose script keeps failing
# reloads once, then stays as it is (check.mjs). Needs docker; the browser
# runs in a Playwright container.
#
#   scripts/viewer-reload-test/run.sh [PAGE_DIR]   (default: the page built from internal/client/web)
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
PAGE=${1:-}
# The page as served, built from internal/client/web (scripts/viewer-page.sh).
if [ -z "$PAGE" ]; then
  PAGE=$("$ROOT/scripts/viewer-page.sh")
  trap '"$ROOT/scripts/rm-tmp.sh" "$PAGE"' EXIT
fi
PW=mcr.microsoft.com/playwright:v1.56.0-noble
docker run --rm -v "$PAGE":/t:ro -v "$PWD":/s:ro -w /e2e "$PW" \
  sh -c 'cp /s/check.mjs /e2e/t.mjs; npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1; node t.mjs'
