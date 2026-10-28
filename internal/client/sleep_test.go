// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"reminal/internal/session"
)

// What may sleep, what never may, and what a sleep would end — said, never
// lost silently.
func TestSleepVerdict(t *testing.T) {
	idle := sleepFacts{Headless: true, Saved: true, Fg: "claude", Conv: "conv-0123456789ab", Attn: "done"}
	with := func(f func(*sleepFacts)) sleepFacts { c := idle; f(&c); return c }
	cases := []struct {
		name   string
		f      sleepFacts
		block  string
		losses []string
	}{
		{"an idle claude sleeps", idle, "", nil},
		{"claude never sent a message sleeps (nothing to resume)", with(func(f *sleepFacts) { f.Conv, f.NoTurn = "", true }), "", nil},
		{"kept awake on purpose", with(func(f *sleepFacts) { f.Pinned = true }), "you asked", nil},
		{"a terminal tab is the session", with(func(f *sleepFacts) { f.Headless = false }), "open in a terminal", nil},
		{"a save that failed", with(func(f *sleepFacts) { f.Saved = false }), "couldn't save", nil},
		{"an agent resumed only by 'latest'", with(func(f *sleepFacts) { f.Fg = "gemini" }), "can't be resumed exactly", nil},
		{"claude whose conversation is not known", with(func(f *sleepFacts) { f.Conv = "" }), "which conversation", nil},
		{"a turn under way", with(func(f *sleepFacts) { f.Attn = "working" }), "", []string{"the agent's unfinished turn"}},
		{"a question to the person", with(func(f *sleepFacts) { f.Attn = "input" }), "", []string{"the question it was waiting on you for"}},
		{"background shells", with(func(f *sleepFacts) { f.Shells = 1 }), "", []string{"its agent's background work"}},
		{"a job in its shell", with(func(f *sleepFacts) { f.Jobs = []string{"npm"} }), "", []string{"npm, running in it"}},
		{"a terminal attached", with(func(f *sleepFacts) { f.Attached = 1 }), "", []string{"the terminal attached to it"}},
		{"a plain shell", with(func(f *sleepFacts) { f.Fg, f.Conv = "", "" }), "", []string{"what was typed into its shell by hand (variables, aliases)"}},
	}
	for _, c := range cases {
		block, losses := sleepVerdict(c.f)
		if c.block == "" && block != "" || c.block != "" && !strings.Contains(block, c.block) {
			t.Errorf("%s: block %q, want one with %q", c.name, block, c.block)
		}
		if !reflect.DeepEqual(losses, c.losses) {
			t.Errorf("%s: losses %q, want %q", c.name, losses, c.losses)
		}
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd\n", 2); got != "c\nd" {
		t.Fatalf("got %q", got)
	}
	if got := lastLines("a\nb", 5); got != "a\nb" {
		t.Fatalf("got %q", got)
	}
}

// A session asleep is left alone at start, unless its wake was cut short
// just now (then the wake is finished); an older cut-short wake sleeps on.
func TestSleepsOn(t *testing.T) {
	const id = "SLEEPC01"
	now := time.Now()
	if sleepsOn(id, now) {
		t.Fatal("a session not asleep sleeps on")
	}
	_ = session.MarkAsleep(id, session.Asleep{SleptAt: now, Why: "asked"})
	defer session.ClearAsleep(id)
	if !sleepsOn(id, now) {
		t.Fatal("a sleeper is restored at start")
	}
	_ = session.MarkWaking(id, now.Add(-time.Minute))
	if sleepsOn(id, now) {
		t.Fatal("a wake cut short a minute ago is not tried again")
	}
	_ = session.MarkWaking(id, now.Add(-time.Hour))
	if !sleepsOn(id, now) {
		t.Fatal("a wake cut short an hour ago is tried again")
	}
}
