// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"testing"
	"time"
)

// A start's parts are each the time since the part before ended, marked
// once: a second mark of a part changes nothing.
func TestAStartIsMarkedInParts(t *testing.T) {
	startMu.Lock()
	startLast, startPhases = time.Now(), map[string]int64{}
	startMu.Unlock()
	time.Sleep(30 * time.Millisecond)
	StartMark("load")
	time.Sleep(60 * time.Millisecond)
	StartMark("replay")
	StartMark("load") // again: dropped
	got := startPhasesNow()
	if got["load"] < 25 || got["load"] > 1000 {
		t.Errorf("load %d ms, want ~30", got["load"])
	}
	if got["replay"] < 55 || got["replay"] > 1000 {
		t.Errorf("replay %d ms, want ~60 (since load, not since the start)", got["replay"])
	}
	got["load"] = -1
	if startPhasesNow()["load"] == -1 {
		t.Error("startPhasesNow handed out its own map")
	}
}
