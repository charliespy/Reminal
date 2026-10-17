// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The screens each agent CLI really draws with no login, or with one it can
// no longer use — captured from the versions reminal installs (tmux, 120x40;
// testdata/harness-screens) — read as logged out, by that CLI only; and the
// same CLIs at work (logged in, or needing none) do not.
func TestAgentsLoginScreensAsTheyDrawThem(t *testing.T) {
	for _, c := range []struct {
		file, prog string
		out        bool
	}{
		// Cursor CLI 2026.10.14: its first screen, and its sign-in after a key.
		{"cursor-agent", "cursor-agent", true},
		{"cursor-first", "cursor-agent", true},
		// Codex 0.162.1: its sign-in menu; signing in through the browser;
		// a key it can no longer use (its 401 on the first turn, a refusal:
		// harnessKeyRefusals).
		{"codex", "codex", true},
		{"codex-badkey", "codex", true},
		{"codex-expired", "codex", true},
		{"codex-expired-start", "codex", false},
		// Gemini CLI 0.62.0: no way to sign in chosen; its Google sign-in
		// asking for the code; its API key dialog; a key it cannot use.
		{"gemini-first-start", "gemini", true},
		{"gemini-first", "gemini", true},
		{"gemini-badkey", "gemini", true},
		{"gemini-expired", "gemini", true},
		{"gemini-key-80", "gemini", true}, // the same in an 80-column session, wrapped
		{"gemini-expired-start", "gemini", false},
		// Qwen Code 0.24.7: its provider menu, and its ModelStudio step.
		{"qwen", "qwen", true},
		{"qwen-badkey", "qwen", true},
		// opencode 1.18.34 works with no login (its free models): at work.
		{"opencode-noupd", "opencode", false},
	} {
		raw, err := os.ReadFile(filepath.Join("testdata", "harness-screens", c.file+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		// Logged out as noteHarnessHealth reads it with c.prog in front: a
		// sign-in screen, or c.prog refusing the key it was given.
		tail := harnessTail(string(raw), harnessRowsFor(c.prog))
		if got := harnessProblemIn(tail, c.prog) || harnessKeyRefusedBy(tail, c.prog); got != c.out {
			t.Errorf("%s (%s): logged out %v, want %v", c.file, c.prog, got, c.out)
		}
		// Another agent's sign-in screen, seen on this one's (a tool's
		// output): not this one logged out.
		if other := harnessTail(string(raw), harnessRows); c.out && c.prog != "opencode" && (harnessProblemIn(other, "opencode") || harnessKeyRefusedBy(other, "opencode")) {
			t.Errorf("%s: read as opencode's own", c.file)
		}
	}
}

// An agent that comes up from its sign-in screen and then sits still at its
// prompt is read only a few times more (the screen is read when it may have
// changed): it is cleared harnessUpFor after the sign-in was first gone, at
// a read the probe is asked for (harnessReadAt) — never left "logged out",
// its mail held, for good. A notice seen once is confirmed a read later.
func TestALoggedInAgentThatSitsStillIsCleared(t *testing.T) {
	defer func(f func() time.Time) { harnessNow = f }(harnessNow)
	clock := time.Unix(1_800_000_000, 0)
	harnessNow = func() time.Time { return clock }
	a := &Agent{}
	login, _ := os.ReadFile(filepath.Join("testdata", "harness-screens", "codex.txt"))
	ready, _ := os.ReadFile(filepath.Join("testdata", "harness-screens", "codex-expired-start.txt"))
	a.noteHarnessHealth(string(login), "codex", false)
	if at := a.harnessReadAt(); !at.Equal(clock.Add(attnInterval)) {
		t.Fatalf("seen once: a read asked for at %v, want %v", at, clock.Add(attnInterval))
	}
	clock = clock.Add(attnInterval)
	a.noteHarnessHealth(string(login), "codex", false)
	if !a.harnessDown() {
		t.Fatal("its sign-in screen, read twice: not logged out")
	}
	// Logged in: its prompt, read once — and then nothing changes.
	clock = clock.Add(time.Second)
	a.noteHarnessHealth(string(ready), "codex", false)
	at := a.harnessReadAt()
	if !a.harnessDown() || !at.Equal(clock.Add(harnessUpFor)) {
		t.Fatalf("after one read of its prompt: down %v, next read %v; want still down until %v", a.harnessDown(), at, clock.Add(harnessUpFor))
	}
	clock = at
	a.noteHarnessHealth(string(ready), "codex", false)
	if a.harnessDown() {
		t.Error("its prompt still at the read asked for: still logged out")
	}
	if at := a.harnessReadAt(); !at.IsZero() {
		t.Errorf("nothing waits on a read now, but one is asked for at %v", at)
	}
}
