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
// count here: what was left running in the background is leftRunning's.
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

// restBase is the processes on term's terminal when its agent came to rest
// (Agent.noteRestBase): what runs with an agent at its prompt — itself, its
// MCP servers, what launched them. A turn it began before it ever rested
// (turn, else zero) may have started work: what started at or after it is
// left out. nil when the terminal can't be read.
func restBase(term *pty.Session, turn time.Time) map[procKey]bool {
	return baseOf(termMembers(listProcs(), term.Pid()), procStartOf(), turn)
}

// procStartOf reads when a process started: to the tick from its /proc
// start where there is one, else as the kernel reports it.
func procStartOf() func(procRow) (time.Time, bool) {
	boot, precise := procBoot()
	return func(p procRow) (time.Time, bool) {
		if precise {
			return startedSince(boot, p.start)
		}
		return session.ProcStartTime(p.pid)
	}
}

// startTick is how close two start times may be and still be taken as the
// same moment: a clock tick, the coarsest /proc counts in.
const startTick = 10 * time.Millisecond

// baseOf is members as a baseline, those started at or after turn (when not
// zero, a tick early counted as at it) left out, and those whose start can't
// be read: left out, a sleep asks rather than drops.
func baseOf(members []procRow, startOf func(procRow) (time.Time, bool), turn time.Time) map[procKey]bool {
	if members == nil {
		return nil
	}
	base := make(map[procKey]bool, len(members))
	for _, p := range members {
		if !turn.IsZero() {
			if t, ok := startOf(p); !ok || !t.Before(turn.Add(-startTick)) {
				continue
			}
		}
		base[p.key()] = true
	}
	return base
}

// procBoot is when the machine booted, to a tick, from /proc/uptime (now
// less how long it has been up): /proc/stat's btime is whole seconds, which
// with a start's whole seconds can read a process up to two seconds older
// than it is. false where there is no /proc.
func procBoot() (time.Time, bool) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return time.Time{}, false
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return time.Time{}, false
	}
	up, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Now().Add(-time.Duration(up * float64(time.Second))), true
}

// startedSince is when a process whose /proc start (ticks since boot) is
// ticks started, boot being when the machine booted.
func startedSince(boot time.Time, ticks string) (time.Time, bool) {
	n, err := strconv.ParseInt(ticks, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return boot.Add(time.Duration(n) * startTick), true
}

// leftRunning names what runs on term's terminal in the background, past
// the shell's own jobs (otherJobs, and what they started): a command left
// going, however soon after it started, under the agent or not (one started
// with & inside a tool call outlives that call's shell and is reparented
// away, still on this terminal, which a sleep hangs up). With agent in front
// and base (what was on the terminal when it came to rest: itself, its MCP
// servers), what it started since counts, base aside, and its own helpers
// started before its first turn, when that is known (own: ownHelpers); with
// no agent in front, whatever is left there, as after an agent was quit.
// seen is false when the terminal's processes could not be read: then
// nothing is named, and nothing can be said to be left.
func leftRunning(term *pty.Session, agent string, base map[procKey]bool, own ownTurn) (names []string, seen bool) {
	prog, _, fg, _ := restoreForeground(term)
	if agent == "" || prog != agent || base == nil {
		fg, base = 0, nil
	}
	procs := listProcs()
	if fg > 0 && own.known {
		base = ownHelpers(base, procs, fg, procStartOf(), own.turn)
	}
	return strays(procs, term.Pid(), fg, base), termMembers(procs, term.Pid()) != nil
}

// ownHelpers is base with what agent (its process, and the leader of its
// group) started for itself before its first turn (turn; zero when it has
// had none): the MCP servers it brings up, and what they started, which can
// come up after it first rests. What it is asked to do starts in a turn, at
// or after turn, and stays out. A start not read stays out too.
func ownHelpers(base map[procKey]bool, procs []procRow, agent int, startOf func(procRow) (time.Time, bool), turn time.Time) map[procKey]bool {
	parent := make(map[int]int, len(procs))
	for _, p := range procs {
		parent[p.pid] = p.ppid
	}
	under := func(pid int) bool {
		for i := 0; i < len(procs) && pid > 1; i++ { // bounded: a cycle read mid-change ends
			if pid = parent[pid]; pid == agent {
				return true
			}
		}
		return false
	}
	out := make(map[procKey]bool, len(base))
	for k := range base {
		out[k] = true
	}
	for _, p := range procs {
		if p.zombie || !under(p.pid) {
			continue
		}
		if t, ok := startOf(p); ok && (turn.IsZero() || t.Before(turn.Add(-startTick))) {
			out[p.key()] = true
		}
	}
	return out
}

// strays names, once each, the processes on shell's terminal that are not
// in base, not the shell's children, and not in the process group of one
// of them (a job and what it started) other than agentPgrp's, the agent's.
func strays(procs []procRow, shell, agentPgrp int, base map[procKey]bool) []string {
	jobs := map[int]bool{}
	for _, p := range procs {
		if p.ppid == shell && p.pgid != agentPgrp {
			jobs[p.pgid] = true
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, p := range termMembers(procs, shell) {
		if base[p.key()] || p.ppid == shell || jobs[p.pgid] || seen[p.comm] {
			continue
		}
		seen[p.comm] = true
		out = append(out, p.comm)
	}
	return out
}

// termMembers are the live processes whose terminal is shell's, the shell
// aside: everything a hangup of that terminal reaches, of any kind. nil when
// the shell's terminal can't be read.
func termMembers(procs []procRow, shell int) []procRow {
	tty := ""
	for _, p := range procs {
		if p.pid == shell {
			tty = p.tty
		}
	}
	if tty == "" || tty == "0" || strings.Trim(tty, "?") == "" {
		return nil
	}
	out := []procRow{}
	for _, p := range procs {
		if p.tty == tty && p.pid != shell && !p.zombie {
			out = append(out, p)
		}
	}
	return out
}

type procRow struct {
	pid, ppid, pgid int
	comm            string
	zombie          bool
	tty             string // its controlling terminal (tty_nr, or ps's name)
	start           string // when it started, as the kernel keeps it
}

// procKey is one process for as long as it lives: its pid, and when it
// started (a pid freed and reused is another).
type procKey struct {
	pid   int
	start string
}

func (p procRow) key() procKey { return procKey{p.pid, p.start} }

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
		if r, ok := parseSleepProcStat(string(b)); ok {
			out = append(out, r)
		}
	}
	return out
}

// parseSleepProcStat reads /proc/<pid>/stat: "pid (comm) state ppid pgrp session
// tty_nr … starttime …". comm may hold spaces and parentheses, so it ends at
// the last ')'.
func parseSleepProcStat(s string) (procRow, bool) {
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
	r := procRow{pid: pid, ppid: ppid, pgid: pgid, comm: s[open+1 : end], zombie: f[0] == "Z"}
	if len(f) > 19 {
		r.tty, r.start = f[4], f[19]
	}
	return r, true
}

func procFromPS() []procRow {
	b, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,pgid=,stat=,tty=,lstart=,comm=").Output()
	if err != nil {
		return nil
	}
	return parsePS(string(b))
}

// parsePS reads `ps -o pid=,ppid=,pgid=,stat=,tty=,lstart=,comm=`: lstart is
// five words ("Tue Oct 27 18:40:34 2026"), comm the rest of its line (a path,
// perhaps with spaces).
func parsePS(s string) []procRow {
	var out []procRow
	for _, l := range strings.Split(s, "\n") {
		f := strings.Fields(l)
		if len(f) < 11 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		pgid, e3 := strconv.Atoi(f[2])
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out = append(out, procRow{
			pid: pid, ppid: ppid, pgid: pgid, zombie: strings.HasPrefix(f[3], "Z"),
			tty: f[4], start: strings.Join(f[5:10], " "), comm: filepath.Base(strings.Join(f[10:], " ")),
		})
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
