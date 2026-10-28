// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"reflect"
	"testing"
)

func TestParseProcStat(t *testing.T) {
	r, ok := parseProcStat("4242 (node (MCP) srv) S 100 4242 100 34816 4242 4194560")
	if !ok || r.pid != 4242 || r.ppid != 100 || r.pgid != 4242 || r.comm != "node (MCP) srv" || r.zombie {
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
	rows := parsePS("  100     1   100 Ss   /bin/zsh\n  200   100   200 S+   /Users/me/My Tools/claude\n  300   100   300 Z    sleep\n")
	if len(rows) != 3 || rows[1].comm != "claude" || rows[1].ppid != 100 || !rows[2].zombie {
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

// Shells under an agent are found however deep, its other helpers not.
func TestShellsUnder(t *testing.T) {
	procs := []procRow{
		{pid: 20, ppid: 10, comm: "claude"},
		{pid: 21, ppid: 20, comm: "node"},
		{pid: 22, ppid: 21, comm: "sh"},
		{pid: 23, ppid: 20, comm: "bash"},
		{pid: 24, ppid: 23, comm: "sleep"},
		{pid: 25, ppid: 20, comm: "zsh", zombie: true},
		{pid: 30, ppid: 10, comm: "bash"}, // not under the agent
	}
	if got := shellsUnder(procs, 20); !reflect.DeepEqual(got, []int{23, 22}) && !reflect.DeepEqual(got, []int{22, 23}) {
		t.Errorf("shells under the agent: %v", got)
	}
}
