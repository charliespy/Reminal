// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"sync"
	"time"
)

// How a headless agent's start went, in parts, for whoever started it: its
// handshake says them (writeHandshake: "phases", ms), and a restore logs
// them. On a quarter of a CPU a restored session took 13 s before it was
// reachable, and which part took them was not known.
var (
	processStart = time.Now()
	startMu      sync.Mutex
	startLast    = processStart
	startPhases  = map[string]int64{}
)

// StartMark ends the start's part named name: the time since the part
// before it ended (or since the process began), in ms. Once each; later
// marks of the same name are dropped.
func StartMark(name string) {
	startMu.Lock()
	defer startMu.Unlock()
	if _, done := startPhases[name]; done {
		return
	}
	now := time.Now()
	startPhases[name] = now.Sub(startLast).Milliseconds()
	startLast = now
}

// startPhasesNow is a copy of the parts marked so far.
func startPhasesNow() map[string]int64 {
	startMu.Lock()
	defer startMu.Unlock()
	out := make(map[string]int64, len(startPhases))
	for k, v := range startPhases {
		out[k] = v
	}
	return out
}
