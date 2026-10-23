# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Harshal Gajjar
#
# Samples, once a second, the CPU and memory of the processes that make up a
# mirrored window on this box: the session (agent), the daemon, and what
# they spawn (ffmpeg, import, xdotool), each tagged with the phase run.sh is
# in (/shared/phase). The relay is test infrastructure and is left out.
import json, os, time
HZ = os.sysconf("SC_CLK_TCK")
PAGE = os.sysconf("SC_PAGE_SIZE")
def role(cmd):
    if not cmd: return None
    exe = os.path.basename(cmd[0])
    if exe == "reminal":
        if len(cmd) > 1 and cmd[1] == "relay": return None
        if len(cmd) > 1 and cmd[1] == "daemon": return "daemon"
        if len(cmd) > 1 and cmd[1] in ("mcp", "new", "list", "info"): return None
        return "agent"
    if exe in ("ffmpeg", "import", "xdotool", "convert"): return exe
    return None
prev = {}
out = open("/shared/procstat.jsonl", "a", buffering=1)
while True:
    t = time.time()
    phase = open("/shared/phase").read().strip() if os.path.exists("/shared/phase") else ""
    by = {}
    for pid in os.listdir("/proc"):
        if not pid.isdigit(): continue
        try:
            cmd = open(f"/proc/{pid}/cmdline", "rb").read().split(b"\0")
            cmd = [x.decode(errors="replace") for x in cmd if x]
            r = role(cmd)
            if not r: continue
            st = open(f"/proc/{pid}/stat").read().rsplit(")", 1)[1].split()
            ticks = int(st[11]) + int(st[12])
            # What it spawned and waited for (import, xdotool: gone before a
            # sample would see them) is counted in its children's time.
            kids = int(st[13]) + int(st[14])
            rss = int(st[21]) * PAGE
        except (FileNotFoundError, ProcessLookupError, IndexError, ValueError):
            continue
        p = prev.get(pid)
        cpu = 0.0 if p is None else (ticks - p[0]) / HZ / (t - p[1]) * 100
        kcpu = 0.0 if p is None else (kids - p[2]) / HZ / (t - p[1]) * 100
        prev[pid] = (ticks, t, kids)
        a = by.setdefault(r, {"cpu": 0.0, "rss": 0, "n": 0})
        a["cpu"] += cpu; a["rss"] += rss; a["n"] += 1
        if r in ("agent", "daemon"):
            k = by.setdefault(r + "-children", {"cpu": 0.0, "rss": 0, "n": 0})
            k["cpu"] += kcpu; k["n"] += 1
    out.write(json.dumps({"t": t, "phase": phase, "by": by}) + "\n")
    time.sleep(max(0.0, 1.0 - (time.time() - t)))
