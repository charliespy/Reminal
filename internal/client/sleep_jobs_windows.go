// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build windows

package client

import (
	"strings"

	"reminal/internal/pty"
)

// otherJobs are the programs running in term's shell besides the coding
// agent (see the Unix one): a console has no job control to tell them
// apart, so it is every child of the shell but the one the agent runs
// under (itself, or the shim host that started it).
func otherJobs(term *pty.Session) []string {
	var out []string
	for _, k := range childProcesses(uint32(term.Pid())) {
		if p, _, _ := agentAmong([]childProc{k}, 2); p != "" {
			continue
		}
		out = append(out, strings.TrimSuffix(strings.ToLower(k.exe), ".exe"))
	}
	return out
}
