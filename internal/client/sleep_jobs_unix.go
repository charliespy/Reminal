// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"reminal/internal/pty"
)

// otherJobs are the programs running in term's shell besides its foreground
// job: a server started with &, a build left going. Sleep closes the
// terminal, which would end them, so a session with any stays awake. Only
// the shell's own children count: everything under the foreground job is
// the agent's (its tool commands, its MCP servers, which a wake starts
// again), and a process that left the session (setsid, a daemon) is not in
// the shell's tree to be ended.
func otherJobs(term *pty.Session) []string {
	shell, fg := term.Pid(), term.ForegroundPgrp()
	var out []string
	for _, p := range listProcs() {
		if p.ppid != shell || p.zombie {
			continue
		}
		if fg > 0 && fg != shell && p.pgid == fg {
			continue // the foreground job: the agent
		}
		out = append(out, p.comm)
	}
	return out
}

type procRow struct {
	pid, ppid, pgid int
	comm            string
	zombie          bool
}

// listProcs is every process on the machine: from /proc where there is one
// (a container has no ps), else from ps.
func listProcs() []procRow {
	if _, err := os.Stat("/proc/self/stat"); err == nil {
		return procFromProc()
	}
	return procFromPS()
}

func procFromProc() []procRow {
	ents, _ := os.ReadDir("/proc")
	var out []procRow
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		if r, ok := parseProcStat(string(b)); ok {
			out = append(out, r)
		}
	}
	return out
}

// parseProcStat reads /proc/<pid>/stat: "pid (comm) state ppid pgrp …".
// comm may hold spaces and parentheses, so it ends at the last ')'.
func parseProcStat(s string) (procRow, bool) {
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 0 || end < open {
		return procRow{}, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(s[:open]))
	if err != nil {
		return procRow{}, false
	}
	f := strings.Fields(s[end+1:])
	if len(f) < 3 {
		return procRow{}, false
	}
	ppid, err1 := strconv.Atoi(f[1])
	pgid, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil {
		return procRow{}, false
	}
	return procRow{pid: pid, ppid: ppid, pgid: pgid, comm: s[open+1 : end], zombie: f[0] == "Z"}, true
}

func procFromPS() []procRow {
	b, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,stat=,comm=").Output()
	if err != nil {
		return nil
	}
	return parsePS(string(b))
}

// parsePS reads `ps -o pid=,ppid=,pgid=,stat=,comm=`: comm is the rest of
// its line (a path, perhaps with spaces).
func parsePS(s string) []procRow {
	var out []procRow
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		pgid, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		comm := strings.Join(f[4:], " ")
		out = append(out, procRow{pid: pid, ppid: ppid, pgid: pgid, comm: filepath.Base(comm), zombie: strings.HasPrefix(f[3], "Z")})
	}
	return out
}
