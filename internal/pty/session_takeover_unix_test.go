// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux || darwin

package pty

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

// A shell a hot restart took over is known by its pid and by which process
// that pid named then: once the pid names another (the shell died, was
// reaped, and the pid went to a new process), it is not taken for the
// shell, nor hung up.
func TestAHotRestartsShellIsKnownByWhichProcessItIs(t *testing.T) {
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	ended := make(chan struct{})
	go func() { _ = child.Wait(); close(ended) }()
	defer func() { _ = child.Process.Kill() }()

	s := Attach(nil, child.Process.Pid) // this process's own child, as a restart's shell is
	if got := s.ShellPid(); got != child.Process.Pid {
		t.Fatalf("its shell: %d, want %d", got, child.Process.Pid)
	}
	// As if the pid were another process's now.
	other := Attach(nil, child.Process.Pid)
	other.shellMark = "a process started at another time"
	if got := other.ShellPid(); got != 0 {
		t.Fatalf("a pid naming another process was taken for the shell: %d", got)
	}
	other.Hangup()
	select {
	case <-ended:
		t.Fatal("the process now at the shell's pid was hung up")
	case <-time.After(300 * time.Millisecond):
	}
	// Control: the shell it took over is hung up.
	s.Hangup()
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the shell it took over was not hung up")
	}
}

// A pid the old image named that is not this process's child (a restart
// keeps the pid, so its shell is) is not taken for the shell.
func TestAPidNotThisProcesssChildIsNotTakenForAHotRestartsShell(t *testing.T) {
	if got := Attach(nil, os.Getppid()).ShellPid(); got != 0 {
		t.Fatalf("this process's parent (%d) was taken for its shell", got)
	}
}
