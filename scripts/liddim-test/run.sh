#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Builds the darwin/arm64 binaries for one or more git refs and runs check.sh
# against each inside a macOS Parallels VM. Nothing runs on this machine.
#
#   scripts/liddim-test/run.sh "<vm name>" HEAD              # this tree
#   scripts/liddim-test/run.sh "<vm name>" v3.16.6 HEAD      # a control first
set -euo pipefail
VM=$1; shift
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
W=$(mktemp -d); trap 'chmod -R u+w "$W" 2>/dev/null; "$ROOT/scripts/rm-tmp.sh" "$W"' EXIT
GOOS=darwin GOARCH=arm64 go build -C "$ROOT/scripts/liddim-test/fakehelper" -o "$W/fake/fake-helper" .
for ref in "$@"; do
  set_=$(echo "$ref" | tr -c 'A-Za-z0-9.\n' _); src="$W/src-$set_"; mkdir -p "$src" "$W/$set_"
  if [ "$ref" = HEAD ]; then (cd "$ROOT" && git ls-files -z | xargs -0 tar cf - | tar xf - -C "$src"); else git -C "$ROOT" archive "$ref" | tar x -C "$src"; fi
  GOOS=darwin GOARCH=arm64 go build -C "$src" -o "$W/$set_/reminal-dev" ./cmd/reminal
  GOOS=darwin GOARCH=arm64 go build -C "$src" -ldflags "-X main.version=9.9.9" -o "$W/$set_/reminal-rel" ./cmd/reminal
  swiftc -O -target arm64-apple-macos12.3 -D REMINAL_TEST_HOOKS -o "$W/$set_/reminal-capture-test" "$src/native/reminal-capture/main.swift"
  swiftc -O -target arm64-apple-macos12.3 -o "$W/$set_/reminal-capture-rel" "$src/native/reminal-capture/main.swift"
  "$ROOT/scripts/rm-tmp.sh" "$src"
done
cp "$ROOT/scripts/liddim-test/check.sh" "$W/"
tar czf - -C "$W" . | prlctl exec "$VM" 'rm -rf /tmp/liddim-test; mkdir -p /tmp/liddim-test && tar xzf - -C /tmp/liddim-test'
rc=0
for ref in "$@"; do
  set_=$(echo "$ref" | tr -c 'A-Za-z0-9.\n' _)
  echo "######## $ref"
  prlctl exec "$VM" "bash /tmp/liddim-test/check.sh $set_ 2>&1 | grep -E '^(==|PASS|FAIL|fails)'" | tee "$W/out" || true
  grep -q '^fails=0$' "$W/out" || rc=1
done
prlctl exec "$VM" 'pkill -9 -f "/tmp/liddim-test/.+/(reminal|fake-helper)"; rm -rf /tmp/liddim-test'
exit $rc
