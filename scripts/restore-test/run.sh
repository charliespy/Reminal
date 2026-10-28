#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# `reminal restore` across a "reboot", in Docker: a relay, and a box whose
# home is a volume — restarting the box kills every process (no signal
# reaches them, as in a power cut) and keeps the disk. Nothing on the host
# is touched.
#
#   scripts/restore-test/run.sh          # build, then run check.sh
#   scripts/restore-test/run.sh shared   # build, then run check-shared.sh
#   scripts/restore-test/run.sh shutdown [MODE]   # build, then run check-shutdown.sh
#   scripts/restore-test/run.sh hostile  # build, then run check-hostile-args.sh
#   scripts/restore-test/run.sh early    # build, then run check-early.sh
#   scripts/restore-test/run.sh stop     # build, then run check-stop-one.sh
#   RESTORE_NAME=… scripts/restore-test/run.sh down   # remove a run kept with KEEP=1
#
# Its containers, network, image and home volume are this run's own
# (RESTORE_NAME, by default from its pid, the time and a random part; one
# given must start reminal-restore-), so two runs at once never remove each
# other's; they are removed when it ends, or kept for a look with KEEP=1 (it
# says their RESTORE_NAME). `down` with no RESTORE_NAME removes what runs
# before this left under the old fixed names, saying each.
set -e
DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$DIR/../.." && pwd)
SAY=
if [ "$1" = down ] && [ -z "${RESTORE_NAME:-}" ]; then
    # What runs before RESTORE_NAME left, under the fixed names they all
    # shared: each said as it is removed.
    RESTORE_NAME=reminal-restore SAY=1
    BOXC=reminal-restore-box RELAYC=reminal-restore-relay HOMEV=reminal-restore-home NET=reminal-restore-net IMG=reminal-restore-test
else
    RESTORE_NAME=${RESTORE_NAME:-reminal-restore-$$-$(date +%s)-$(od -An -N3 -tx1 /dev/urandom | tr -d ' \n')}
    case "$RESTORE_NAME" in
    reminal-restore-?*) ;;
    *) echo "RESTORE_NAME must start reminal-restore-: $RESTORE_NAME" >&2; exit 2 ;;
    esac
    BOXC=$RESTORE_NAME-box RELAYC=$RESTORE_NAME-relay HOMEV=$RESTORE_NAME-home NET=$RESTORE_NAME-net IMG=$RESTORE_NAME-img
fi

# rmhas removes one thing of this run's (kind: container, volume, network,
# image), if it is there — saying so for the old set.
rmhas() {
    if docker "$1" inspect "$2" >/dev/null 2>&1; then
        case "$1" in
        container) docker rm -f "$2" >/dev/null ;;
        image) docker rmi -f "$2" >/dev/null ;;
        *) docker "$1" rm "$2" >/dev/null ;;
        esac
        if [ -n "$SAY" ]; then echo "removed $1 $2"; fi
    fi
}
down() {
    rmhas container "$BOXC"
    rmhas container "$RELAYC"
    rmhas volume "$HOMEV"
    rmhas network "$NET"
}
# down, and its image: all of it, as it ends (the image is built before the
# containers start, so not by the down that clears the way for them).
gone() {
    down
    rmhas image "$IMG"
}
if [ "$1" = down ]; then gone; echo stopped; exit 0; fi
if ! docker info >/dev/null 2>&1; then echo "Docker isn't running." >&2; exit 1; fi

OUT=$(mktemp -d)
# Its own removed as it ends, however it ends — kept for a look with KEEP=1.
trap '"$ROOT/scripts/rm-tmp.sh" "$OUT"; if [ -n "${KEEP:-}" ]; then echo "kept: RESTORE_NAME=$RESTORE_NAME $0 down"; else gone; fi' EXIT
cp "$DIR/Dockerfile" "$DIR/mcp.sh" "$OUT/"
docker run --rm -v "$ROOT":/src:ro -v "$OUT":/out \
    -v reminal-restore-gocache:/root/.cache/go-build -v reminal-restore-gomod:/go/pkg/mod \
    -e GOFLAGS=-buildvcs=false -e CGO_ENABLED=0 -w /src \
    golang:1.26-bookworm sh -c 'go build -o /out/reminal ./cmd/reminal && go build -o /out/claude ./scripts/restore-test/fakeclaude'
docker build -q -t "$IMG" "$OUT" >/dev/null

down
docker network create "$NET" >/dev/null
docker volume create "$HOMEV" >/dev/null
docker run -d --name "$RELAYC" --network "$NET" "$IMG" reminal relay 8080 >/dev/null
docker run -d --name "$BOXC" --init --network "$NET" -v "$HOMEV":/root \
    -e REMINAL_RELAY=ws://$RELAYC:8080/ws -e REMINAL_WEB=http://$RELAYC:8080 \
    "$IMG" >/dev/null
# The checks are told this run's names; run, not exec'd, so its own are
# removed after them (the trap), their status its own.
export BOX="$BOXC" RELAY="$RELAYC"
case "$1" in
shared) "$DIR/check-shared.sh" ;;
shutdown) "$DIR/check-shutdown.sh" "$2" ;;
hostile) "$DIR/check-hostile-args.sh" ;;
early) "$DIR/check-early.sh" ;;
stop) "$DIR/check-stop-one.sh" ;;
*) "$DIR/check.sh" ;;
esac
