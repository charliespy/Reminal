// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"testing"
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
		// a key it can no longer use (its 401 on the first turn).
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
		if got := harnessProblemIn(harnessTail(string(raw), harnessRowsFor(c.prog)), c.prog); got != c.out {
			t.Errorf("%s (%s): logged out %v, want %v", c.file, c.prog, got, c.out)
		}
		// Another agent's sign-in screen, seen on this one's (a tool's
		// output): not this one logged out.
		if c.out && c.prog != "opencode" && harnessProblemIn(harnessTail(string(raw), harnessRows), "opencode") {
			t.Errorf("%s: read as opencode's own", c.file)
		}
	}
}
