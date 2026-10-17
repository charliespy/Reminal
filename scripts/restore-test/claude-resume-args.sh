#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# A claude started with a system prompt comes back on its OWN conversation:
# here the real claude (npm @anthropic-ai/claude-code, not logged in) parses
# what a restore types for such sessions. Resumed by its id, it answers "No
# conversation found with session ID: <id>" (the id is made up); a flag kept
# bare before --resume took --resume as its value instead, and claude went
# on to a new session ("Not logged in"). Docker only.
set -eu
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
OUT=$(mktemp -d)
trap '"$ROOT/scripts/rm-tmp.sh" "$OUT"' EXIT
GOMOD=${CLAUDE_ARGS_GOMOD:-reminal-claude-args-gomod}
docker run --rm -v "$ROOT":/src:ro -v "$OUT":/out -v "$GOMOD":/go/pkg/mod -e REMINAL_CLAUDE_ARGS=/out/args.json \
  golang:1.26-bookworm sh -c 'cp -r /src /w && cd /w && go test -count=1 -run TestClaudeResumeArgsForTheRig ./internal/client/ >/dev/null'
cat > "$OUT/check.sh" <<'SH'
# As a user, not root: claude refuses --dangerously-skip-permissions as root.
export HOME=/tmp/h; mkdir -p $HOME
export NPM_CONFIG_PREFIX=$HOME/npm PATH=$HOME/npm/bin:$PATH
npm i -g @anthropic-ai/claude-code >/dev/null 2>&1
echo "claude $(claude --version)"
fails=0
node -e 'for (const l of JSON.parse(require("fs").readFileSync("/out/args.json","utf8"))) console.log(l.Started + "\t" + l.Typed)' > /tmp/lines
while IFS="$(printf '\t')" read -r started typed; do
  said=$(eval "timeout 40 $typed -p hi" 2>&1 | head -1)
  case "$said" in
    "No conversation found with session ID: "*) echo "ok   started as: $started"; echo "     resumed as: $typed — claude resumes it by its id" ;;
    *) echo "FAIL started as: $started"; echo "     resumed as: $typed — claude said: $said"; fails=$((fails+1)) ;;
  esac
done < /tmp/lines
[ $fails -eq 0 ] && echo "all passed" || { echo "$fails failed"; exit 1; }
SH
docker run --rm --user node -v "$OUT":/out node:22-bookworm-slim sh /out/check.sh
