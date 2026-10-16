// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"errors"
	"os"
	"testing"
	"time"

	"reminal/internal/session"
)

// A record write that has checked "not paused" must not land after a pause
// that cleared the record: the stopped session would be listed as sharing
// again. The write here is held open in that window while Ctrl-] (pause)
// arrives.
func TestPauseWinsOverARecordWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	a := &Agent{sessionID: "PAUSERACE", startedAt: time.Now(), webURL: "http://127.0.0.1:1"}
	a.recordActive(0)
	if _, err := session.ReadActiveByID(a.sessionID); err != nil {
		t.Fatalf("the record wasn't written to start with: %v", err)
	}

	inWindow := make(chan struct{})
	release := make(chan struct{})
	a.afterPausedCheck = func() {
		close(inWindow)
		<-release
	}
	wrote := make(chan struct{})
	go func() {
		a.recordActive(0)
		close(wrote)
	}()
	<-inWindow
	a.afterPausedCheck = nil
	paused := make(chan struct{})
	go func() {
		a.pause()
		close(paused)
	}()
	time.Sleep(100 * time.Millisecond) // let pause run as far as it can
	close(release)
	<-wrote
	<-paused

	if _, err := session.ReadActiveByID(a.sessionID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("after pause the record is still there (err %v): a write that checked first landed after it", err)
	}
	a.recordActive(0)
	if _, err := session.ReadActiveByID(a.sessionID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a paused session wrote its record again: %v", err)
	}
}
