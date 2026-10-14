// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"strings"
	"testing"
)

// A note a restore asks for is shown under the restore banner, ahead of
// the plan's own: made safe, and never left in the environment for the
// session's shell to inherit.
func TestARestoreShowsTheNoteItWasAskedFor(t *testing.T) {
	t.Setenv(envRestoreNote, "Your session came back from 09:14:\x1b[31m the disk was full when it slept.")
	lead := restoreNoteEnv()
	if strings.ContainsAny(lead, "\x1b") || !strings.HasPrefix(lead, "Your session came back from 09:14:") {
		t.Errorf("the note, made safe: %q", lead)
	}
	if _, set := os.LookupEnv(envRestoreNote); set {
		t.Error("the note was left in the environment")
	}
	run, note := withRestoreNote(lead, func() (string, string) { return "claude --resume x", "" })()
	if run != "claude --resume x" || note != lead {
		t.Errorf("no note of its own: %q, %q", run, note)
	}
	_, note = withRestoreNote(lead, func() (string, string) { return "", "pick this session's" })()
	if note != lead+" — pick this session's" {
		t.Errorf("with a note of its own: %q", note)
	}
	if _, note = withRestoreNote("", func() (string, string) { return "", "its own" })(); note != "its own" {
		t.Errorf("no note asked for: %q", note)
	}
	if got := noteSafe("abc\u202edef\u2066ghi\u2069", 200); got != "abcdefghi" {
		t.Errorf("bidirectional controls kept: %q", got)
	}
	if long := noteSafe(strings.Repeat("x", 500), 200); len([]rune(long)) > 201 {
		t.Errorf("a note of %d characters", len([]rune(long)))
	}
}

// The note goes to the session restored with it, never to one started from
// it: spawnEnv leaves it out.
func TestTheRestoreNoteIsNotInherited(t *testing.T) {
	t.Setenv(envRestoreNote, "a note")
	for _, kv := range spawnEnv() {
		if strings.HasPrefix(kv, envRestoreNote+"=") {
			t.Fatal("spawnEnv passed the note on")
		}
	}
}
