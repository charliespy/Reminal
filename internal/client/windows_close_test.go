// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"testing"
	"time"
)

// goneBackend is a host whose mirrored window has just closed: a listing
// comes back fine and without it.
type goneBackend struct{ windowBackend }

func (goneBackend) list() ([]winInfo, error) { return nil, nil }
func (goneBackend) exists(string) bool       { return false }

// streamEnds reports whether checkWindow ends the stream within d, called the
// way the capture loop calls it: every frame interval, with the picture moving.
func streamEnds(s *winStream, d time.Duration) bool {
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if !s.checkWindow(nil, true) {
			return true
		}
	}
	return false
}

// A window closed from the viewer's menu must leave its pane as soon as it is
// gone. On the idle liveness schedule that took three missed checks 2.4s
// apart on a Mac, so the closed window's pane hung on for seconds.
func TestAskedCloseEndsTheStreamAtOnce(t *testing.T) {
	for _, path := range []struct {
		name      string
		capNative bool
	}{{"listing probe", true}, {"existence check", false}} {
		t.Run(path.name, func(t *testing.T) {
			a := &Agent{}
			s := &winStream{a: a, b: goneBackend{}, w: winInfo{ID: "w1", W: 800, H: 600}, capNative: path.capNative, lastGeoCheck: time.Now()}
			if streamEnds(s, 600*time.Millisecond) {
				t.Fatal("a window missing once, with no close asked for, ended the stream: the idle schedule's tolerance is gone")
			}
			a.noteCloseAsked("w1")
			start := time.Now()
			if !streamEnds(s, time.Second) {
				t.Fatal("the stream outlived its window by a second after the viewer asked to close it")
			}
			if took := time.Since(start); took > 3*winClosePoll {
				t.Fatalf("the pane took %v to go after its window did; want within a poll or two", took)
			}
		})
	}
}

// The ask only speeds things up for a while: a window that stays (an app
// asking whether to save) goes back to the idle schedule.
func TestCloseAskExpires(t *testing.T) {
	a := &Agent{}
	a.noteCloseAsked("w1")
	s := &winStream{a: a, w: winInfo{ID: "w1"}}
	if !s.closeAsked() {
		t.Fatal("a fresh ask was not seen")
	}
	a.winMu.Lock()
	a.winCloseAsked["w1"] = time.Now().Add(-winCloseWatch)
	a.winMu.Unlock()
	if s.closeAsked() {
		t.Fatal("an ask older than winCloseWatch still counts")
	}
	if (&winStream{a: a, w: winInfo{ID: "w2"}}).closeAsked() {
		t.Fatal("an ask for one window counted for another")
	}
}
