#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The upload toast (check.mjs): its bar follows what has left the browser,
# "Saving" until the machine's ack, then where the file landed; a batch with
# a file too large says so and stays; a session left mid-upload says so and
# fades. The page as served, no host behind it (a socket and its acks stood
# in for). Needs docker; nothing here touches the host's reminal.
#
# SRC=<tree> builds the page from another tree (a control: main fails).
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
SRC=${SRC:-$ROOT}
PAGE=$("$SRC/scripts/viewer-page.sh")
trap '"$ROOT/scripts/rm-tmp.sh" "$PAGE"' EXIT
PW=mcr.microsoft.com/playwright:v1.56.0-noble
docker run --rm -v "$PAGE":/t:ro -v "$PWD":/s:ro -w /e2e "$PW" \
  sh -c 'cp /s/check.mjs /e2e/t.mjs; npm i --silent --no-save playwright@1.56.0 >/dev/null 2>&1; node t.mjs'
