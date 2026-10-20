// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"os"
	"testing"
	"time"
)

// A record's PID that now names a process started 30 s after the writer —
// a restart within a minute of a session's start, its old PID taken by a
// process of the new boot — is another process: the session is gone, and is
// restored. The wide tolerance once let it pass as the same, and the session
// never came back.
func TestAPIDTakenAgainSoonAfterIsAnotherProcess(t *testing.T) {
	// The start-time compare (macOS, Windows), run on every platform.
	steady := startTimeSteady
	startTimeSteady = true
	t.Cleanup(func() { startTimeSteady = steady })
	self := os.Getpid()
	start, ok := procStartTime(self)
	if !ok {
		t.Skip("no process start times on this platform")
	}
	if (Active{PID: self, PidStartedAt: start}).reusedPID() {
		t.Fatal("the writer itself read as another process")
	}
	if (Active{PID: self, PidStartedAt: start.Add(-time.Second)}).reusedPID() {
		t.Fatal("a second's rounding read as another process")
	}
	if !(Active{PID: self, PidStartedAt: start.Add(-30 * time.Second)}).reusedPID() {
		t.Fatal("a PID taken again 30 s after its writer began read as the same process: its session would never be restored")
	}
	// Read earlier than it wrote: still the writer, never pruned for it.
	if (Active{PID: self, PidStartedAt: start.Add(30 * time.Second)}).reusedPID() {
		t.Fatal("a live session read as another process for a clock stepped back")
	}
	// A record from before PidStartedAt: the wide tolerance, as before.
	if (Active{PID: self, StartedAt: start.Add(-30 * time.Second)}).reusedPID() {
		t.Fatal("a legacy record lost its tolerance")
	}
}
