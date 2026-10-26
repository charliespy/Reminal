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
# at version 9.9.9, served from inside the container. Needs docker; builds
# run in a golang container too.
# A heavy run: waits for one of the machine's slots first (scripts/README-gates.md).
[ -n "${REMINAL_GATE_HELD:-}" ] || [ ! -f "$(dirname -- "$0")/../gatelock.py" ] || ! command -v python3 >/dev/null || exec python3 "$(dirname -- "$0")/../gatelock.py" -- bash "$0" "$@"
set -euo pipefail
cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')
# Docker volumes are named from REMINAL_VOLUME_PREFIX (default reminal), so a
# run can keep its caches apart from other checkouts on the machine.
V=${REMINAL_VOLUME_PREFIX:-reminal}
W=$(mktemp -d)
trap '"$ROOT/scripts/rm-tmp.sh" "$W"' EXIT
mkdir -p "$W/old" "$W/new"
if [ -n "${1:-}" ]; then
  git -C "$ROOT" worktree add -q --detach "$W/src" "$1"
  trap 'git -C "$ROOT" worktree remove --force "$W/src"; "$ROOT/scripts/rm-tmp.sh" "$W"' EXIT
  OLDSRC=$W/src
else OLDSRC=$ROOT; fi
# build SRC VERSION OUT: reminal from SRC, saying it is VERSION, at OUT (under $W).
build() {
  docker run --rm -v "$1":/src:ro -v "$W":/out -v "$V-gomod":/go/pkg/mod -v "$V-gocache":/root/.cache/go-build \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -e GOARCH="$ARCH" -w /src golang:1.26-bookworm \
    go build -ldflags "-X main.version=$2" -o "/out/$3" ./cmd/reminal
}
build "$OLDSRC" 3.0.0 old/reminal
cp inside.sh "$W/"
# release VERSION: the release v9.9.9, its build saying it is VERSION, offered
# to the old version and accepted. Prints what inside.sh saw.
release() {
  build "$ROOT" "$1" new/reminal
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
