// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reminal/internal/crypto"
	"reminal/internal/pty"
)

// quietShell is a restored session whose shell is the script body (it
// prints its prompt, then echoes what is typed: cat on a terminal), its
// output pumped into the session as a real one's is.
func quietShell(t *testing.T, body string) *Agent {
	t.Helper()
	sh := filepath.Join(t.TempDir(), "sh")
	if err := os.WriteFile(sh, []byte("#!/bin/sh\n"+body+"\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	term, err := pty.Start(sh)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	key, err := crypto.NewSessionKey()
	if err != nil {
		t.Fatal(err)
	}
	box, err := crypto.NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{box: box, buf: newScrollback(1 << 20), term: term, restoreRun: "echo resumed-42"}
	go a.pumpPTY()
	return a
}

// shown is what the session has recorded, in order, as text.
func (a *Agent) shown(t *testing.T) string {
	t.Helper()
	a.buf.mu.Lock()
	entries := append([]scrollEntry(nil), a.buf.entries...)
	a.buf.mu.Unlock()
	var b strings.Builder
	for _, e := range entries {
		p, err := a.box.Decrypt(e.Data)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(p)
	}
	return b.String()
}

func waitShown(t *testing.T, a *Agent, s string, within time.Duration) time.Duration {
	t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		if strings.Contains(a.shown(t), s) {
			return time.Since(start)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%q not shown within %v; shown: %q", s, within, a.shown(t))
	return 0
}

// The new shell's prompt is usually drawn while the restore waits for the
// session's first try at the relay, so it is already there when the resume
// looks for it: the resume is typed once the screen is quiet, not after the
// wait's 10 s deadline.
func TestARestoreTypesItsResumeSoonWhenThePromptIsAlreadyDrawn(t *testing.T) {
	a := quietShell(t, `printf 'ready$ '`)
	waitShown(t, a, "ready$ ", 5*time.Second)
	time.Sleep(500 * time.Millisecond) // drawn well before the restore looks
	a.signalRegistered()
	began := time.Now()
	a.restoreStart()
	if took := time.Since(began); took > 3*time.Second {
		t.Fatalf("the resume was typed %v after the restore began (the prompt was already drawn)", took.Round(10*time.Millisecond))
	}
	waitShown(t, a, "echo resumed-42", 2*time.Second)
}

// Control: a shell still starting (its profile running, nothing printed
// yet) is not typed into: the resume waits for its prompt.
func TestARestoreWaitsForTheShellsPromptBeforeTypingItsResume(t *testing.T) {
	a := quietShell(t, `sleep 1.5; printf 'ready$ '`)
	a.signalRegistered()
	a.restoreStart()
	waitShown(t, a, "echo resumed-42", 2*time.Second)
	out := a.shown(t)
	if p, r := strings.Index(out, "ready$ "), strings.Index(out, "echo resumed-42"); p < 0 || r < p {
		t.Fatalf("the resume was typed before the shell's prompt: %q", out)
	}
}
