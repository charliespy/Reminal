// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"testing"
	"time"

	"reminal/internal/session"
)

// Claude Code's StopFailure, in Stop's place when an API error ended the
// turn: "done", the error kept beside it, the turn's start kept; the next
// turn's start, and a plain Stop after it, say no failure.
func TestATurnEndedByAnErrorSaysWhich(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REMINAL_SESSION", "FAIL2345")

	hookWithPayload(t, "working", `{"hook_event_name":"UserPromptSubmit"}`)
	began := session.ReadHookStateAnyAge("FAIL2345").TurnAt
	time.Sleep(5 * time.Millisecond)
	hookWithPayload(t, "failed", `{"hook_event_name":"StopFailure","error":"rate_limit","error_details":"429"}`)
	hs := session.ReadHookStateAnyAge("FAIL2345")
	if hs == nil || hs.State != "done" || hs.Failure != "rate_limit" || !hs.TurnAt.Equal(began) {
		t.Fatalf("after StopFailure: %+v (turn began %v)", hs, began)
	}

	hookWithPayload(t, "working", `{"hook_event_name":"UserPromptSubmit"}`)
	if hs := session.ReadHookStateAnyAge("FAIL2345"); hs.Failure != "" {
		t.Fatalf("a new turn kept the last one's failure: %+v", hs)
	}
	hookWithPayload(t, "done", `{"hook_event_name":"Stop"}`)
	if hs := session.ReadHookStateAnyAge("FAIL2345"); hs.State != "done" || hs.Failure != "" {
		t.Fatalf("a plain Stop: %+v", hs)
	}

	// What it names is kept only as a short word.
	for payload, want := range map[string]string{
		`{"error":"authentication_failed"}`: "authentication_failed",
		`{"error":""}`:                      "unknown",
		`{"error":"rate limit; rm -rf /"}`:  "unknown",
		`not json`:                          "unknown",
	} {
		if got := hookFailure([]byte(payload)); got != want {
			t.Errorf("%s: %q, want %q", payload, got, want)
		}
	}
}
