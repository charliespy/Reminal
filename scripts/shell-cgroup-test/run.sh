#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# REMINAL_SHELL_CGROUP end to end, in Docker: a container with a private
# cgroup namespace and a writable cgroup v2 mount (--privileged), so it can
# make cgroups of its own. Checks
#   1. the gated test: a shell started by the session host is born in the
#      cgroup, and the host itself is not;
#   2. a real `reminal new` with REMINAL_SHELL_CGROUP set: the session's shell
#      is in that cgroup (its /proc/<pid>/cgroup), the session host is not;
#   3. a directory that is not a cgroup: the session still starts.
#
#   scripts/shell-cgroup-test/run.sh            # this tree
#   TREE=<tree> scripts/shell-cgroup-test/run.sh
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TREE=${TREE:-$(CDPATH= cd -- "$DIR/../.." && pwd)}
docker run --rm --privileged --cgroupns=private -v "$TREE":/src:ro \
  -v reminal-gomod:/go/pkg/mod -v reminal-gocache:/root/.cache/go-build \
  -e GOFLAGS=-buildvcs=false golang:1.26-bookworm sh -c '
set -u
cp -a /src /w && cd /w
fail=0
echo "== the gated test"
REMINAL_CGROUP_TEST=1 go test -count=1 -run TestTheShellIsStartedInTheShellCgroup -v ./internal/pty/ 2>&1 | grep -E "^(--- |ok|FAIL)" || true
REMINAL_CGROUP_TEST=1 go test -count=1 -run TestTheShellIsStartedInTheShellCgroup ./internal/pty/ >/dev/null 2>&1 || fail=1
go build -o /usr/local/bin/reminal ./cmd/reminal
export HOME=/root SHELL=/bin/bash REMINAL_RELAY=ws://127.0.0.1:1/ws REMINAL_WEB=http://127.0.0.1:1
# shellof <session pid>: the pid of its shell (its child).
shellof() { for c in /proc/[0-9]*; do [ "$(awk "{print \$4}" $c/stat 2>/dev/null)" = "$1" ] && { echo ${c#/proc/}; return; }; done; }
pidof_session() { reminal new "$@" 2>&1 | sed -n "s/.*PID: *\([0-9]*\).*/\1/p" | head -1; }
echo "== reminal new into a cgroup"
mkdir -p /sys/fs/cgroup/reminal-e2e
host=$(REMINAL_SHELL_CGROUP=/sys/fs/cgroup/reminal-e2e pidof_session incg)
sh=""; for i in $(seq 1 50); do sh=$(shellof "$host"); [ -n "$sh" ] && break; sleep 0.1; done
echo "session host $host: $(cat /proc/$host/cgroup 2>/dev/null)"
echo "its shell $sh: $(cat /proc/$sh/cgroup 2>/dev/null)"
[ "$(cat /proc/$sh/cgroup 2>/dev/null)" = "0::/reminal-e2e" ] || { echo "FAIL the shell is not in the cgroup"; fail=1; }
[ "$(cat /proc/$host/cgroup 2>/dev/null)" != "0::/reminal-e2e" ] || { echo "FAIL the session host moved into the cgroup"; fail=1; }
tr "\0" "\n" < /proc/$sh/environ | grep -q "^REMINAL_SHELL_CGROUP=" && { echo "FAIL the shell inherited REMINAL_SHELL_CGROUP"; fail=1; }
echo "== reminal new with a directory that is not a cgroup"
host2=$(REMINAL_SHELL_CGROUP=/tmp pidof_session nocg)
sh2=""; for i in $(seq 1 50); do sh2=$(shellof "$host2"); [ -n "$sh2" ] && break; sleep 0.1; done
echo "session host $host2, its shell $sh2: $(cat /proc/$sh2/cgroup 2>/dev/null)"
[ -n "$sh2" ] || { echo "FAIL no shell when the directory is not a cgroup"; fail=1; }
[ $fail = 0 ] && echo "PASS" || { echo "FAIL"; exit 1; }
'
