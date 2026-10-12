// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"testing"
	"time"
)

// A record goes on naming the agent a session had until a person ends it.
// A restored session holds onto it only until that agent is running again:
// holding it past that point meant quitting the agent inside the settle
// window left the record still naming it, and the next restart brought back
// an agent the person had deliberately closed. And an agent that left with
// nobody typing was not quit by anyone: a machine going down stops it unasked
// — for as long as reminal outlasts it: a restart once brought 18 sessions
// back with a bare prompt, their records having forgotten claude.
func TestHoldPreviousAgent(t *testing.T) {
	cases := []struct {
		name      string
		restoring bool
		agentSeen bool
		atPrompt  bool
		// typed: a person typed since the agent was last seen running (or,
		// never seen, since the session started).
		typed bool
		since time.Duration
		// sinceAgent: how long ago a save last saw the agent; long ago
		// unless the case is about the moment it left.
		sinceAgent time.Duration
		want       bool
	}{
		// Not at a prompt: something else is in front for a moment (a pager the
		// agent opened). That is not the agent ending, whatever else is true.
		{"a pager is in front, normal session", false, false, false, true, time.Hour, time.Hour, true},
		{"a pager is in front, just restored", true, false, false, true, time.Second, time.Hour, true},

		// At the shell's own prompt, mid-restore: the agent has not been
		// started again yet, and sessions restored after this one still need
		// to know what it was running.
		{"restoring, agent not back yet", true, false, true, false, 5 * time.Second, time.Hour, true},

		// The agent came back, the person quit it.
		{"restoring, agent seen, then quit", true, true, true, true, 5 * time.Second, time.Hour, false},
		// …but gone with nobody typing, it was not quit.
		{"restoring, agent seen, gone, nobody typed", true, true, true, false, 5 * time.Second, time.Hour, true},

		// The settle window ends on its own if the agent never appears and a
		// person is using the shell; with nobody there, the next restart
		// tries again.
		{"restoring, agent never appeared, window passed, typed", true, false, true, true, restoreSettle + time.Second, time.Hour, false},
		{"restoring, agent never appeared, window passed, nobody typed", true, false, true, false, restoreSettle + time.Second, time.Hour, true},

		// An ordinary session at its prompt, a person using it: nothing runs.
		{"not restoring, at prompt, typed", false, false, true, true, time.Hour, time.Hour, false},
		{"not restoring, agent seen, at prompt, typed", false, true, true, true, time.Hour, time.Hour, false},

		// The machine shutting down: the agent quit a moment before reminal
		// did, and a save landed in between. It is still the agent to bring
		// back.
		{"agent left a moment ago, at prompt", false, true, true, true, time.Hour, restoreSaveEvery, true},
		{"restoring, agent seen, left a moment ago", true, true, true, true, 5 * time.Second, time.Second, true},
		// Gone for longer, a person typing: it was quit.
		{"agent left past the grace, typed", false, true, true, true, time.Hour, agentGoneGrace, false},
		// Gone for longer, nobody typing: a machine going down, reminal
		// outlasting its agent past the grace. Still the agent to bring back.
		{"agent left past the grace, nobody typed", false, true, true, false, time.Hour, agentGoneGrace, true},
		{"agent left an hour ago, nobody typed", false, true, true, false, time.Hour, time.Hour, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := holdPreviousAgent(c.restoring, c.agentSeen, c.atPrompt, c.typed, c.since, c.sinceAgent); got != c.want {
				t.Fatalf("holdPreviousAgent(restoring=%v, seen=%v, atPrompt=%v, typed=%v, since=%v, sinceAgent=%v) = %v, want %v",
					c.restoring, c.agentSeen, c.atPrompt, c.typed, c.since, c.sinceAgent, got, c.want)
			}
		})
	}
}
