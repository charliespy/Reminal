// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"reminal/internal/session"
)

// What a restore types is text and nothing else: a flag whose value holds a
// control character is left out (a terminal acts on those before a shell sees
// any quote), and the note says which.
func TestResumeLeavesOutWhatATerminalWouldActOn(t *testing.T) {
	useHelp(t)
	r := session.Restore{ID: "A", Fg: "claude", Cwd: "/w", Conv: "f19308b1-5c22-4b55-96d9-e1803b9501b6",
		FgArgs: []string{"claude", "--model", "opus\x1b[201~\x15more", "--dangerously-skip-permissions"}}
	argv, note := resumePlan(r, []session.Restore{r})
	want := []string{"claude", "--dangerously-skip-permissions", "--resume", r.Conv}
	if !reflect.DeepEqual(argv, want) {
		t.Fatalf("got %q, want %q", argv, want)
	}
	if !strings.Contains(note, "--model") || hasControl(note) {
		t.Fatalf("note %q: want it to name the flag left out, as text", note)
	}
	for _, a := range argv {
		if hasControl(a) {
			t.Fatalf("%q holds a control character", a)
		}
	}
}

// A conversation id that is not one is never typed.
func TestResumeTypesOnlyAConversationID(t *testing.T) {
	useHelp(t)
	r := session.Restore{ID: "A", Fg: "claude", FgArgs: []string{"claude"}, Cwd: "/w", Conv: "abcdefgh\x15ijkl"}
	argv, _ := resumePlan(r, []session.Restore{r})
	if !reflect.DeepEqual(argv, []string{"claude", "--continue"}) {
		t.Fatalf("got %q, want claude --continue", argv)
	}
}

// PowerShell ends a single-quoted string at ' and at its typographic forms;
// each is doubled to stand for itself.
func TestPowerShellQuotesEveryQuote(t *testing.T) {
	v := "x\u2019; y \u2018z\u201a\u201b'w"
	got := shellCommand([]string{"claude", "--model", v}, `C:\Program Files\PowerShell\7\pwsh.exe`)
	want := "claude --model 'x\u2019\u2019; y \u2018\u2018z\u201a\u201a\u201b\u201b''w'"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// cmd expands %VAR% even inside quotes: a flag holding % is left out there.
func TestCmdLeavesOutPercent(t *testing.T) {
	useHelp(t)
	r := session.Restore{ID: "A", Fg: "claude", Cwd: "/w", Conv: "f19308b1-5c22-4b55-96d9-e1803b9501b6",
		FgArgs: []string{"claude", "--model", "%COMSPEC%", "--dangerously-skip-permissions"}}
	argv, note := resumePlanFor(r, []session.Restore{r}, `C:\Windows\System32\cmd.exe`)
	if !reflect.DeepEqual(argv, []string{"claude", "--dangerously-skip-permissions", "--resume", r.Conv}) || !strings.Contains(note, "--model") {
		t.Fatalf("cmd: got %q %q", argv, note)
	}
	// Elsewhere it is kept, quoted.
	if argv, note := resumePlanFor(r, []session.Restore{r}, "/bin/zsh"); len(argv) != 6 || note != "" {
		t.Fatalf("zsh: got %q %q", argv, note)
	}
}

// A paste never carries its own end marker: one inside it would end the
// paste early and the rest be read as keys pressed.
func TestPasteNeverCarriesItsEndMarker(t *testing.T) {
	a := &Agent{}
	a.bracketedPaste.Store(true)
	out := a.wrapPaste([]byte("claude --append-system-prompt 'x\x1b[201~y'"))
	if !bytes.HasPrefix(out, pasteStart) || !bytes.HasSuffix(out, pasteEnd) || bytes.Count(out, pasteEnd) != 1 {
		t.Fatalf("pasted %q", out)
	}
}

// What is printed into a terminal as a note is text.
func TestNoteSafe(t *testing.T) {
	if got := noteSafe("a\x1b]52;c;aGk=\x07b\x15c", 100); got != "a]52;c;aGk=bc" {
		t.Fatalf("got %q", got)
	}
	if got := noteSafe(strings.Repeat("x", 20), 5); got != "xxxxx…" {
		t.Fatalf("got %q", got)
	}
}
