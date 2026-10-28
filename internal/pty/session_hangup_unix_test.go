// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package pty

import (
	"os"
	"runtime/pprof"
	"testing"
	"time"
)

// A session stopped while its output is read (a stop from outside, with
// reminal reading the shell as it always is) hangs its shell up, as a
// terminal closing does: the shell, idle at its prompt, ends, and the read
// under way ends with it. Close alone did not: the master, a blocking
// descriptor, stays open under the read, and the shell ran on.
func TestAStoppedSessionHangsUpItsIdleShell(t *testing.T) {
	s, err := Start("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	read := make(chan struct{})
	go func() {
		defer close(read)
		b := make([]byte, 256)
		for {
			if _, err := s.Read(b); err != nil {
				return
			}
		}
	}()
	time.Sleep(300 * time.Millisecond) // idle at its prompt
	s.Hangup()
	_ = s.Close()
	waited := make(chan error, 1)
	go func() { waited <- s.Wait() }()
	select {
	case <-waited:
	case <-time.After(3 * time.Second):
		_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
		t.Fatal("closed, its idle shell was not hung up: still running after 3 s")
	}
	select {
	case <-read:
	case <-time.After(3 * time.Second):
		t.Fatal("its shell ended, the read under way did not")
	}
}
