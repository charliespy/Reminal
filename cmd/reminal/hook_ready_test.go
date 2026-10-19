// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"testing"
	"time"

	"reminal/internal/session"
)

// Claude Code's SessionStart says it is up, reading its input: kept as when,
// apart from its turn's state — which it leaves as it was, never read as a
// turn begun — and gone with the session.
func TestAHarnessSayingItIsReadyIsNotATurn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REMINAL_SESSION", "RDYR2345")

	before := time.Now()
	hookWithPayload(t, "ready", `{"session_id":"3f2c9a10-7d4e-4b8a-9c11-0e5d6f7a8b9c","hook_event_name":"SessionStart","source":"resume"}`)
	at := session.HookReadyAt("RDYR2345")
	if at.Before(before) || at.After(time.Now()) {
		t.Fatalf("ready at %v, said after %v", at, before)
	}
	if hs := session.ReadHookStateAnyAge("RDYR2345"); hs != nil {
		t.Fatalf("being up wrote a turn's state: %+v", hs)
	}

	hookWithPayload(t, "done", `{"hook_event_name":"Stop"}`)
	said := session.ReadHookStateAnyAge("RDYR2345")
	time.Sleep(5 * time.Millisecond) // so a rewritten TS would differ
	hookWithPayload(t, "ready", `{"hook_event_name":"SessionStart","source":"clear"}`)
	if hs := session.ReadHookStateAnyAge("RDYR2345"); hs == nil || hs.State != "done" || !hs.TS.Equal(said.TS) {
		t.Fatalf("being up changed the turn's state: %+v, was %+v", hs, said)
	}
	if !session.HookReadyAt("RDYR2345").After(at) {
		t.Fatal("said again, its time not kept")
	}

	if err := session.ClearHookState("RDYR2345"); err != nil {
		t.Fatal(err)
	}
	if !session.HookReadyAt("RDYR2345").IsZero() {
		t.Fatal("still ready once the session is gone")
	}
}

// Claude Code is integrated with its SessionStart, said as ready.
func TestClaudeCodeSaysWhenItIsReady(t *testing.T) {
	for _, a := range agentTargets() {
		if a.Bin != "claude" {
			continue
		}
		for _, e := range a.hooks.events {
			if e.Event == "SessionStart" && e.State == "ready" && e.Matcher == "" {
				return
			}
		}
		t.Fatalf("Claude Code's hooks have no SessionStart → ready: %+v", a.hooks.events)
	}
	t.Fatal("no Claude Code target")
}
