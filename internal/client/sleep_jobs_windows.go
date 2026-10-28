// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build windows

package client

import (
	"strings"

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

// agentShells: a console's processes carry no start-up to tell the shells
// an agent's MCP servers run through from ones it left going, so none are
// counted here; what its hook says (or that it said nothing) stands.
func agentShells(*pty.Session, string) int { return 0 }
