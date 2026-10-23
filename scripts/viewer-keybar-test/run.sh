#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The command bar's ^X, ^S and ^Q (check.mjs): the byte each sends, tapped
# on a phone and clicked on a desktop, its Ctrl key to a remote window, that
# each looks like ^O, and that the bar stays one row. Needs docker; the
# browser runs in a Playwright container.
#
#   scripts/viewer-keybar-test/run.sh [PAGE_DIR] [SHOTS_DIR]   (screenshots of the bar into SHOTS_DIR)
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
PAGE=${1:-}
if [ -z "$PAGE" ]; then
  PAGE=$("$ROOT/scripts/viewer-page.sh")
  trap '"$ROOT/scripts/rm-tmp.sh" "$PAGE"' EXIT
fi
OUT=()
[ -n "${2:-}" ] && OUT=(-v "$2":/out)
PW=mcr.microsoft.com/playwright:v1.56.0-noble
docker run --rm -v "$PAGE":/t:ro -v "$PWD":/s:ro ${OUT[@]+"${OUT[@]}"} -w /e2e "$PW" \
  sh -c 'cp /s/check.mjs /e2e/t.mjs; npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1; node t.mjs'
