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
	"time"

	"reminal/internal/proc"
	"reminal/internal/pty"
	"reminal/internal/session"
)

// otherJobs are the programs running in term's shell besides agent, the
// coding agent its record wakes ("" for none): a server started with &, a
// build left going, or whatever is in front when that is not the agent (a
// command run after quitting it). Sleep closes the terminal, which would end
// them, so a session with any stays awake. Only the shell's own children
// count: everything under the agent is its own (its tool commands, its MCP
// servers, which a wake starts again), and a process that left the session
// (setsid, a daemon) is not in the shell's tree to be ended.
func otherJobs(term *pty.Session, agent string) []string {
	prog, _, fg, _ := restoreForeground(term)
	return shellJobs(listProcs(), term.Pid(), fg, agent != "" && prog == agent)
}

// shellJobs are the names of shell's live children, those in process group
// fg left out when fgIsAgent, each named once.
func shellJobs(procs []procRow, shell, fg int, fgIsAgent bool) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range procs {
		if p.ppid != shell || p.zombie || fgIsAgent && fg > 0 && p.pgid == fg || seen[p.comm] {
			continue
		}
		seen[p.comm] = true
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

// exited says process pid has ended: gone, or a zombie no one has reaped
// yet (in a container whose first process does not reap, an ended session
// stays one, and is still "alive" to a signal).
func exited(pid int) bool {
	if !proc.Alive(pid) {
		return true
	}
	for _, p := range listProcs() {
		if p.pid == pid {
			return p.zombie
		}
	}
	return true
}

// agentShells counts the shells running under agent, when it is the program
// in front of term, that it started after its own start-up: at rest an agent
// runs none but what its MCP servers were launched through (npx's sh), all
// started with it, so a later one is a command it left going in the
// background, whether or not it said so.
func agentShells(term *pty.Session, agent string) int {
	prog, _, pid, _ := restoreForeground(term)
	if agent == "" || prog != agent {
		return 0
	}
	began, ok := session.ProcStartTime(pid)
	if !ok {
		return 0
	}
	n := 0
	for _, s := range shellsUnder(listProcs(), pid) {
		if t, ok := session.ProcStartTime(s); ok && t.After(began.Add(agentStartup)) {
			n++
		}
	}
	return n
}

// agentStartup is how long after an agent starts the shells it starts with
// (its MCP servers') are taken to come.
const agentStartup = 30 * time.Second

// shellsUnder are the live shells among root's descendants.
func shellsUnder(procs []procRow, root int) []int {
	kids := map[int][]procRow{}
	for _, p := range procs {
		kids[p.ppid] = append(kids[p.ppid], p)
	}
	var out []int
	for todo := []int{root}; len(todo) > 0; {
		pid := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		for _, k := range kids[pid] {
			if k.pid == pid {
				continue
			}
			if !k.zombie && isLoginShell(k.comm) {
				out = append(out, k.pid)
			}
			todo = append(todo, k.pid)
		}
	}
	return out
}
