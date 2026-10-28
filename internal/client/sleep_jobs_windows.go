// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build windows

package client

import (
	"strings"
	"time"

	"reminal/internal/proc"
	"reminal/internal/pty"
)

// otherJobs are the programs running in term's shell besides agent, the
// coding agent its record wakes (see the Unix one): a console has no job
// control to tell them apart, so it is every child of the shell but the one
// that agent runs under (itself, or the shim host that started it).
func otherJobs(term *pty.Session, agent string) []string {
	var out []string
	for _, k := range childProcesses(uint32(term.Pid())) {
		if p, _, _ := agentAmong([]childProc{k}, 2); agent != "" && p == agent {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.ToLower(k.exe), ".exe"))
	}
	return out
}

// exited says process pid has ended.
func exited(pid int) bool { return !proc.Alive(pid) }

// A console's processes are not read by their terminal here yet (its process
// list or job object would be the way), so what an agent left running is
// what its hook says (or that it said nothing): no baseline is kept.
type procKey struct{}

func restBase(*pty.Session, time.Time) map[procKey]bool { return nil }

func leftRunning(*pty.Session, string, map[procKey]bool) []string { return nil }
