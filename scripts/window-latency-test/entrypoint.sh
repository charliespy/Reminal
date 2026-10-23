#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# The desktop: Xvfb (with -fbdir when CAPTURE=fbdir, so the window backend
# reads the screen from its file instead of running import per frame),
# openbox, and the probe window. Then it waits; run.sh drives it.
set -e
FB=""
if [ "${CAPTURE:-fbdir}" = fbdir ]; then mkdir -p /tmp/fb; FB="-fbdir /tmp/fb"; fi
Xvfb :99 -screen 0 1280x800x24 -nolisten tcp $FB >/tmp/xvfb.log 2>&1 &
for _ in $(seq 50); do xdpyinfo >/dev/null 2>&1 && break; sleep 0.1; done
openbox >/tmp/openbox.log 2>&1 &
for _ in $(seq 100); do wmctrl -m >/dev/null 2>&1 && break; sleep 0.1; done
# A window mapped the moment openbox claims the screen can be lost while it
# is still setting up: give it a second, and start the probe again once if
# it has not been managed after 5 s.
sleep 1
for attempt in 1 2; do
  python3 /usr/local/bin/probe.py >/tmp/probe.out 2>&1 &
  for _ in $(seq 50); do wmctrl -l 2>/dev/null | grep -q latency-probe && break; sleep 0.1; done
  wmctrl -l | grep -q latency-probe && break
  kill $! 2>/dev/null || true
done
wmctrl -l | grep -q latency-probe || { echo "probe window never appeared"; cat /tmp/probe.out; exit 1; }
echo ready > /tmp/desktop-ready
exec sleep infinity
