// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package pty

import (
	"sync"
	"syscall"
	"testing"
	"time"
)

// A session closed while one goroutine reads it and another asks it about
// itself (a signal ending reminal while its attention probe runs, or a
// viewer resizes it): the asks
// see it closed, never the master's fd destroyed or reused under them. Run
// it under -race: an ioctl on os.File.Fd() raced the close.
func TestASessionClosedWhileAskedAboutIsNotRacedOnItsFd(t *testing.T) {
	for i := 0; i < 20; i++ {
		s, err := Start("/bin/sh")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		stop := make(chan struct{})
		wg.Add(2)
		go func() { // its output read, as reminal reads it
			defer wg.Done()
			b := make([]byte, 256)
			for {
				if _, err := s.Read(b); err != nil {
					return
				}
			}
		}()
		go func() { // asked about, as the attention probe asks it
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = s.ForegroundPgrp()
				_ = s.Pid()
				_ = s.Resize(80, 24) // a viewer's resize
				_, _, _ = s.Getsize()
			}
		}()
		time.Sleep(5 * time.Millisecond)
		_ = s.Close()
		time.Sleep(5 * time.Millisecond)
		close(stop)
		// Its shell gone, the read under way ends: nothing is left running.
		if p := s.Pid(); p > 0 {
			_ = syscall.Kill(p, syscall.SIGKILL)
		}
		_ = s.Wait()
		wg.Wait()
		if p := s.ForegroundPgrp(); p != 0 {
			t.Fatalf("closed, its foreground still read as %d", p)
		}
	}
}
