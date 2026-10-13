// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"os"
	"testing"

	"reminal/internal/session"
)

// A hook records a conversation for its own session only: one run inside
// session A with REMINAL_SESSION set to B's id would otherwise choose what B
// types when it is restored.
func TestConvAllowed(t *testing.T) {
	for _, c := range []struct {
		enclosing, id string
		want          bool
	}{
		{"", "BBBB2345", true},         // its session cannot be told: as before
		{"BBBB2345", "BBBB2345", true}, // its own
		{"bbbb2345", "BBBB2345", true},
		{"AAAA2345", "BBBB2345", false}, // another's
	} {
		if got := convAllowed(c.enclosing, c.id); got != c.want {
			t.Errorf("convAllowed(%q, %q) = %v, want %v", c.enclosing, c.id, got, c.want)
		}
	}
}

func TestHookWritesNoOtherSessionsConv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("REMINAL_SESSION", "BBBB2345")
	was := convOwnSession
	convOwnSession = func(id string) bool { return convAllowed("AAAA2345", id) } // running inside AAAA2345
	t.Cleanup(func() { convOwnSession = was })
	feed := func(payload string) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.WriteString(payload)
		_ = w.Close()
		old := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = old })
		_ = runHook([]string{"working"})
	}
	feed(`{"session_id":"f19308b1-5c22-4b55-96d9-e1803b9501b6"}`)
	if got := session.ReadConv("BBBB2345"); got != "" {
		t.Fatalf("a hook in another session wrote BBBB2345's conversation: %q", got)
	}
	convOwnSession = func(id string) bool { return convAllowed("BBBB2345", id) } // its own
	feed(`{"session_id":"f19308b1-5c22-4b55-96d9-e1803b9501b6"}`)
	if got := session.ReadConv("BBBB2345"); got != "f19308b1-5c22-4b55-96d9-e1803b9501b6" {
		t.Fatalf("its own hook did not write it: %q", got)
	}
}
