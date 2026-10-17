#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A restored session on PowerShell types its agent's command with each value
# single-quoted (shellCommand). PowerShell ends such a string at ' and at its
# typographic forms (U+2018, U+2019, U+201A, U+201B), so each must be doubled.
# Here a real PowerShell reads back what shellCommand types for values made
# of each: every one must come back as itself and run nothing. Docker only.
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
OUT=$(mktemp -d)
trap '"$ROOT/scripts/rm-tmp.sh" "$OUT"' EXIT
GOMOD=${PWSH_GOMOD:-reminal-pwsh-gomod}
docker run --rm -v "$ROOT":/src:ro -v "$OUT":/out -v "$GOMOD":/go/pkg/mod -e REMINAL_PWSH_QUOTES=/out/quotes.json \
  golang:1.26-bookworm sh -c 'cp -r /src /w && cd /w && go test -count=1 -run TestPowerShellQuotingForTheRig ./internal/client/ >/dev/null'
cat > "$OUT/check.ps1" <<'PS'
$lines = Get-Content -Raw /out/quotes.json | ConvertFrom-Json
$fails = 0
foreach ($l in $lines) {
  $got = Invoke-Expression $l.Typed
  if ($got -ne $l.Value) { Write-Output ("FAIL read back as something else: " + $l.Typed); $fails++ }
}
foreach ($n in 1..5) { if (Test-Path "/tmp/q$n") { Write-Output "FAIL /tmp/q$n was made: a value ran a command"; $fails++ } }
if ($fails -eq 0) { Write-Output ("ok   every value (" + $lines.Count + ") read back as itself, nothing run"); Write-Output "all passed" } else { exit 1 }
PS
docker run --rm -v "$OUT":/out mcr.microsoft.com/powershell:7.4-debian-12 pwsh -NoProfile -File /out/check.ps1
