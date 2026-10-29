// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"reflect"
	"testing"
	"time"
)

func TestParseSleepProcStat(t *testing.T) {
	r, ok := parseSleepProcStat("4242 (node (MCP) srv) S 100 4242 100 34816 4242 4194560 0 0 0 0 1 2 0 0 20 0 1 0 98765 1000 50")
	if !ok || r.pid != 4242 || r.ppid != 100 || r.pgid != 4242 || r.comm != "node (MCP) srv" || r.zombie || r.tty != "34816" || r.start != "98765" {
		t.Fatalf("parsed %+v %v", r, ok)
	}
	if r, ok := parseSleepProcStat("7 (sh) Z 1 7 7"); !ok || !r.zombie {
		t.Fatalf("zombie: %+v %v", r, ok)
	}
	if _, ok := parseSleepProcStat("garbage"); ok {
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

// A baseline taken after a turn the agent began before it ever rested
// leaves out what started at or after that turn (a tick early counts as at
// it): work, not a helper. A start not read is left out too (asked about,
// never dropped).
func TestBaseOfLeavesOutWhatTheFirstTurnStarted(t *testing.T) {
	launch := time.Date(2026, 10, 27, 18, 0, 0, 0, time.UTC)
	turn := launch.Add(2*time.Second + 400*time.Millisecond)
	starts := map[int]time.Time{
		20: launch,                             // the agent
		21: launch.Add(500 * time.Millisecond), // its MCP server
		22: turn.Add(time.Second),              // what the turn started
		23: turn.Add(-5 * time.Millisecond),    // within a tick of the turn
		25: turn.Add(-300 * time.Millisecond),  // well before it
	}
	startOf := func(p procRow) (time.Time, bool) { s, ok := starts[p.pid]; return s, ok }
	members := []procRow{{pid: 20, start: "a"}, {pid: 21, start: "b"}, {pid: 22, start: "c"}, {pid: 23, start: "d"}, {pid: 24, start: "e"}, {pid: 25, start: "f"}}
	base := baseOf(members, startOf, turn)
	if !base[procKey{20, "a"}] || !base[procKey{21, "b"}] || !base[procKey{25, "f"}] || len(base) != 3 {
		t.Errorf("base after an early turn: %v", base)
	}
	if base := baseOf(members, startOf, time.Time{}); len(base) != 6 {
		t.Errorf("base with no turn first: %v", base)
	}
	if baseOf(nil, startOf, turn) != nil {
		t.Error("an unread terminal gave a baseline")
	}
}

// A /proc start is read to the tick against a boot time to the tick: never
// whole seconds, which read a process up to two seconds older than it is.
func TestStartedSince(t *testing.T) {
	boot := time.Date(2026, 10, 27, 12, 0, 0, 900_000_000, time.UTC)
	got, ok := startedSince(boot, "12345")
	if want := boot.Add(123*time.Second + 450*time.Millisecond); !ok || !got.Equal(want) {
		t.Fatalf("started %v, want %v", got, want)
	}
	if _, ok := startedSince(boot, "x"); ok {
		t.Fatal("read a start that is not a number")
	}
}

// The agent's own helpers (its MCP servers, and what they start) are its
// baseline when they come up after it first rests, as claude's do, as long
// as they started before its first turn: what a turn starts under it is
// work. A process not under the agent never is a helper.
func TestOwnHelpers(t *testing.T) {
	launch := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	turn := launch.Add(30 * time.Second)
	agent := procRow{pid: 20, ppid: 10, pgid: 20, comm: "claude", start: "a"}
	procs := []procRow{
		{pid: 10, ppid: 1, pgid: 10, comm: "bash", start: "s"},
		agent,
		{pid: 21, ppid: 20, pgid: 20, comm: "reminal", start: "b"},         // its MCP server, up after it rested
		{pid: 22, ppid: 21, pgid: 20, comm: "node", start: "c"},            // …and what that started
		{pid: 23, ppid: 20, pgid: 20, comm: "sh", start: "d"},              // what a turn started under it
		{pid: 24, ppid: 1, pgid: 24, comm: "npm", start: "e"},              // reparented away: not under it
		{pid: 25, ppid: 20, pgid: 20, comm: "x", start: "f", zombie: true}, // ended
		{pid: 26, ppid: 20, pgid: 20, comm: "y", start: "g"},               // a start not read
	}
	starts := map[int]time.Time{10: launch, 20: launch, 21: launch.Add(3 * time.Second), 22: launch.Add(4 * time.Second),
		23: turn.Add(time.Second), 24: launch.Add(5 * time.Second), 25: launch}
	startOf := func(p procRow) (time.Time, bool) { s, ok := starts[p.pid]; return s, ok }
	base := map[procKey]bool{agent.key(): true}
	got := ownHelpers(base, procs, 20, startOf, turn)
	want := map[procKey]bool{agent.key(): true, {21, "b"}: true, {22, "c"}: true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after a turn: %v, want %v", got, want)
	}
	if len(base) != 1 {
		t.Errorf("the baseline it was given changed: %v", base)
	}
	// No turn yet: everything under it that started is its own.
	want[procKey{23, "d"}] = true
	if got := ownHelpers(base, procs, 20, startOf, time.Time{}); !reflect.DeepEqual(got, want) {
		t.Errorf("no turn yet: %v, want %v", got, want)
	}
	// With the agent in front, its MCP server is no stray; what the turn
	// started is.
	tty := "34816"
	for i := range procs {
		procs[i].tty = tty
	}
	if got := strays(procs, 10, 20, ownHelpers(base, procs, 20, startOf, turn)); !reflect.DeepEqual(got, []string{"sh", "npm", "y"}) {
		t.Errorf("strays: %q", got)
	}
}
