// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reminal/internal/session"
)

// A hook tells its session's agent the state is written, so the agent reads
// it at once instead of when its screen next moves.
func TestHookTellsTheAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REMINAL_SESSION", "CCCC2345")
	pid := os.Getpid()
	if err := session.WriteActive(session.Active{ID: "CCCC2345", PID: pid, StartedAt: time.Now(), PidStartedAt: session.SelfStartTime()}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(home, ".reminal", fmt.Sprintf("agent-%d.sock", pid)))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, _ := bufio.NewReader(c).ReadString('\n')
		_, _ = fmt.Fprintln(c, "ok")
		got <- line
	}()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if err := runHook([]string{"done"}); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "attn\n" {
			t.Fatalf("the agent was sent %q, want attn", line)
		}
	case <-time.After(5 * time.Second): // the test's own abort cap
		t.Fatal("the hook did not tell the agent")
	}
	if hs := session.ReadHookStateAnyAge("CCCC2345"); hs == nil || hs.State != "done" {
		t.Fatalf("state %+v, want done", hs)
	}
}

// With no agent listening (gone, or too old to know the verb's socket), the
// hook still writes the state and succeeds.
func TestHookWithNoAgentStillWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REMINAL_SESSION", "DDDD2345")
	if err := session.WriteActive(session.Active{ID: "DDDD2345", PID: os.Getpid(), StartedAt: time.Now(), PidStartedAt: session.SelfStartTime()}); err != nil {
		t.Fatal(err)
	}
	r, w, _ := os.Pipe()
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if err := runHook([]string{"input"}); err != nil {
		t.Fatal(err)
	}
	if hs := session.ReadHookStateAnyAge("DDDD2345"); hs == nil || hs.State != "input" {
		t.Fatalf("state %+v, want input", hs)
	}
}
