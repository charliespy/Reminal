#!/usr/bin/env bash
# What answering yes to reminal's upgrade offer does, in a container: the
# terminal it was asked in must stay open and go on with the new version,
# not close. A terminal that runs reminal itself (an editor's reminal tab)
# closes when reminal exits.
#
#   run.sh [OLD_REF]
#
# The installed ("old") version is built from this tree, or from OLD_REF
# (e.g. origin/main, the control); the release it upgrades to is this tree
# at version 9.9.9, served from inside the container. Needs docker.
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
W=$(mktemp -d)
trap 'rm -rf "${W:?}"' EXIT
mkdir -p "$W/old" "$W/new"
if [ -n "${1:-}" ]; then
  git -C "$ROOT" worktree add -q --detach "$W/src" "$1"
  trap 'git -C "$ROOT" worktree remove --force "$W/src"; rm -rf "${W:?}"' EXIT
  OLDSRC=$W/src
else OLDSRC=$ROOT; fi
GOOS=linux GOARCH=$ARCH go build -C "$OLDSRC" -ldflags "-X main.version=3.0.0" -o "$W/old/reminal" ./cmd/reminal
cp inside.sh "$W/"
# release VERSION: the release v9.9.9, its build saying it is VERSION, offered
# to the old version and accepted. Prints what inside.sh saw.
release() {
  GOOS=linux GOARCH=$ARCH go build -C "$ROOT" -ldflags "-X main.version=$1" -o "$W/new/reminal" ./cmd/reminal
  tar -C "$W/new" -czf "$W/reminal_9.9.9_linux_$ARCH.tar.gz" reminal
  docker run --rm -e ARCH="$ARCH" -v "$W:/t:ro" python:3.12-slim sh /t/inside.sh
}
fail=0
pass() { echo "PASS  $1"; }
no() { echo "FAIL  $1"; fail=1; }
out=$(release 9.9.9)
echo "$out"
grep -q '^INSTALLED .*9\.9\.9' <<<"$out" && pass "yes to the offer installs the new version" || no "the new version was not installed"
if [ -z "${1:-}" ]; then
  grep -q '^TERMINAL open' <<<"$out" && pass "the terminal it was asked in stays open" || no "the terminal closed"
  grep -q '^SESSIONS 1' <<<"$out" && pass "the session it was started for runs" || no "no session runs"
  grep -q '^SESSION_EXE /root/.local/bin/reminal ' <<<"$out" && pass "that session is the new version, in the same terminal" || no "the session is not the installed binary"
  grep -q '^OFFERS 1' <<<"$out" && pass "the new version doesn't offer the upgrade again" || no "the upgrade was offered more than once"
  grep -q 'Starting it' <<<"$out" && pass "it says it is starting the new version" || no "it didn't say so"
  # A build that calls itself older than its release still looks out of date
  # to itself; started straight after installing, it must not offer (and
  # install, and start) the same release again, over and over.
  out=$(release 9.9.8)
  echo "$out"
  grep -q '^OFFERS 1' <<<"$out" && grep -q '^TERMINAL open' <<<"$out" \
    && pass "a build that calls itself older than its release is offered once, then runs" \
    || no "a build that calls itself older than its release was offered again"
else
  # The control: the version before this change.
  grep -q '^TERMINAL closed' <<<"$out" && pass "control: before this change the terminal closes" || no "control: the terminal stayed open before this change"
fi
exit $fail
