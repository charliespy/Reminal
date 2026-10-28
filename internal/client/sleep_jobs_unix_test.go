// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"reflect"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	r, ok := parseProcStat("4242 (node (MCP) srv) S 100 4242 100 34816 4242 4194560 0 0 0 0 1 2 0 0 20 0 1 0 98765 1000 50")
	if !ok || r.pid != 4242 || r.ppid != 100 || r.pgid != 4242 || r.comm != "node (MCP) srv" || r.zombie || r.tty != "34816" || r.start != "98765" {
		t.Fatalf("parsed %+v %v", r, ok)
	}
	if r, ok := parseProcStat("7 (sh) Z 1 7 7"); !ok || !r.zombie {
		t.Fatalf("zombie: %+v %v", r, ok)
	}
	if _, ok := parseProcStat("garbage"); ok {
		t.Fatal("parsed garbage")
	}
}

func TestParsePS(t *testing.T) {
	rows := parsePS("  100     1   100 Ss   ttys003  Tue Oct 27 18:40:34 2026     /bin/zsh\n" +
		"  200   100   200 S+   ttys003  Tue Oct 27 18:41:02 2026     /Users/me/My Tools/claude\n" +
		"  300   100   300 Z    ??       Tue Oct 27 18:42:10 2026     sleep\n")
	if len(rows) != 3 || rows[1].comm != "claude" || rows[1].ppid != 100 || !rows[2].zombie ||
		rows[1].tty != "ttys003" || rows[1].start != "Tue Oct 27 18:41:02 2026" || rows[2].tty != "??" {
		t.Fatalf("parsed %+v", rows)
	}
}

// The job in front is the agent's only when it is the agent the session
// wakes with: a command run after quitting it is a job a sleep would end.
func TestShellJobs(t *testing.T) {
	procs := []procRow{
		{pid: 10, ppid: 1, pgid: 10, comm: "bash"},
		{pid: 20, ppid: 10, pgid: 20, comm: "claude"},
		{pid: 21, ppid: 20, pgid: 20, comm: "node"}, // under the agent: its own
		{pid: 30, ppid: 10, pgid: 30, comm: "npm"},  // started with &
		{pid: 31, ppid: 10, pgid: 30, comm: "npm"},
		{pid: 40, ppid: 10, pgid: 40, comm: "sleep", zombie: true},
	}
	if got := shellJobs(procs, 10, 20, true); !reflect.DeepEqual(got, []string{"npm"}) {
		t.Errorf("agent in front: %q", got)
	}
	if got := shellJobs(procs, 10, 20, false); !reflect.DeepEqual(got, []string{"claude", "npm"}) {
		t.Errorf("something else in front: %q", got)
	}
}

// What was left running in the background on the session's terminal: with
// the agent in front, what is new since it came to rest, under it or
// reparented away, however soon it started; never its MCP servers (there
// at rest), the shell's own jobs and what they started (named as jobs),
// or anything on another terminal.
func TestStrays(t *testing.T) {
	const tty = "34816"
	shell := procRow{pid: 10, ppid: 1, pgid: 10, comm: "bash", tty: tty, start: "1"}
	agent := procRow{pid: 20, ppid: 10, pgid: 20, comm: "claude", tty: tty, start: "2"}
	mcp := procRow{pid: 21, ppid: 20, pgid: 20, comm: "node", tty: tty, start: "3"}
	base := map[procKey]bool{agent.key(): true, mcp.key(): true}
	procs := []procRow{shell, agent, mcp,
		{pid: 22, ppid: 20, pgid: 20, comm: "sh", tty: tty, start: "9"},    // a command it left going, under it
		{pid: 23, ppid: 1, pgid: 23, comm: "npm", tty: tty, start: "9"},    // one started with & in a tool call, reparented
		{pid: 30, ppid: 10, pgid: 30, comm: "make", tty: tty, start: "5"},  // the shell's own job
		{pid: 31, ppid: 30, pgid: 30, comm: "cc", tty: tty, start: "6"},    // …and what it started
		{pid: 40, ppid: 1, pgid: 40, comm: "other", tty: "99", start: "9"}, // another terminal
		{pid: 41, ppid: 1, pgid: 41, comm: "gone", tty: tty, zombie: true}, // ended
		{pid: 21, ppid: 20, pgid: 20, comm: "node", tty: tty, start: "8"},  // a pid reused: another process
	}
	if got := strays(procs, 10, 20, base); !reflect.DeepEqual(got, []string{"sh", "npm", "node"}) {
		t.Errorf("agent in front: %q", got)
	}
	// No agent in front (quit): whatever was left there, its old helpers
	// too, the shell's jobs aside.
	if got := strays(procs[:5], 10, 0, nil); !reflect.DeepEqual(got, []string{"npm"}) {
		t.Errorf("no agent in front: %q", got)
	}
	// A terminal that can't be read: nothing claimed.
	if got := strays([]procRow{{pid: 10, tty: "?"}, {pid: 11, tty: "?"}}, 10, 0, nil); got != nil {
		t.Errorf("unread terminal: %q", got)
	}
}
