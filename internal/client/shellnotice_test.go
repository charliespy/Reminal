// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"testing"

	"reminal/internal/crypto"
)

// The restore's banner, before the new shell's prompt or after it (as the
// session's first try at the relay took), leaves the prompt the cursor's
// line, and no empty row under the banner.
func TestTheRestoreBannerLeavesThePromptTheCursorsLine(t *testing.T) {
	prompt := "user@box:/work$ "
	for _, first := range []string{"banner", "prompt"} {
		k, _ := crypto.NewSessionKey()
		box, _ := crypto.NewBox(k)
		a := &Agent{sessionID: "BANN3456", box: box, buf: newScrollback(1 << 20)}
		a.initScreen()
		t.Cleanup(a.stopAttention)
		a.record([]byte("earlier output\r\n")) // a restored screen, its fresh line taken
		if first == "banner" {
			a.recordAtShell(restoreBanner(false))
			a.record([]byte(prompt))
		} else {
			a.record([]byte(prompt))
			a.recordAtShell(restoreBanner(false))
		}
		p := a.screen.CursorPosition()
		rows := strings.Split(stripANSI(a.screen.Render()), "\n")
		if got := strings.TrimRight(rows[p.Y], " "); got != strings.TrimRight(prompt, " ") || p.X != len(prompt) {
			t.Fatalf("%s first: the cursor is at row %q column %d, want the prompt column %d:\n%s", first, got, p.X, len(prompt), strings.Join(rows, "\n"))
		}
		if !strings.Contains(rows[p.Y-1], "reminal restored this session") {
			t.Fatalf("%s first: the banner is not just above the prompt:\n%s", first, strings.Join(rows, "\n"))
		}
	}
}

// A notice at a shell with keys typed at its prompt: under it, the prompt and
// the keys drawn again, the cursor back after them.
func TestANoticeAtTheShellRedrawsItsLineUnderIt(t *testing.T) {
	k, _ := crypto.NewSessionKey()
	box, _ := crypto.NewBox(k)
	a := &Agent{sessionID: "NOTC3456", box: box, buf: newScrollback(1 << 20)}
	a.initScreen()
	t.Cleanup(a.stopAttention)
	line := "user@box:/work$ ls -l"
	a.record([]byte(line))
	a.recordAtShell("a notice")
	p := a.screen.CursorPosition()
	rows := strings.Split(stripANSI(a.screen.Render()), "\n")
	if got := strings.TrimRight(rows[p.Y], " "); got != line || p.X != len(line) {
		t.Fatalf("the cursor is at row %q column %d, want %q column %d:\n%s", got, p.X, line, len(line), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[p.Y-1], "a notice") {
		t.Fatalf("the notice is not just above the line:\n%s", strings.Join(rows, "\n"))
	}
}

// A notice written over rows that hold something already (a screen drawn
// before, its cursor moved up): the notice's row and the row drawn again
// under it hold only what they are given, nothing of what was there.
func TestANoticeClearsTheRowsItWritesOn(t *testing.T) {
	k, _ := crypto.NewSessionKey()
	box, _ := crypto.NewBox(k)
	a := &Agent{sessionID: "CLRS3456", box: box, buf: newScrollback(1 << 20)}
	a.initScreen()
	t.Cleanup(a.stopAttention)
	a.record([]byte("AAAA\r\nOLD-OLD-OLD-OLD-OLD-OLD\r\nOLD-OLD-OLD-OLD-OLD-OLD\x1b[2A\r$ ls"))
	a.recordAtShell("note")
	rows := strings.Split(stripANSI(a.screen.Render()), "\n")
	p := a.screen.CursorPosition()
	if got := strings.TrimRight(rows[p.Y-1], " "); got != "note" {
		t.Fatalf("the notice's row is %q, want only the notice:\n%s", got, strings.Join(rows, "\n"))
	}
	if got := strings.TrimRight(rows[p.Y], " "); got != "$ ls" || p.X != len("$ ls") {
		t.Fatalf("the row drawn again is %q (cursor column %d), want %q at %d:\n%s", got, p.X, "$ ls", len("$ ls"), strings.Join(rows, "\n"))
	}
}
