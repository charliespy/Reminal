// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// HookProbeEnv marks a program reminal runs only to ask it something (an
// agent's --version): set in its environment, and in everything it starts.
//
// Such a program is a child of the session's own reminal, and so inside the
// session by the process tree (Enclosing). One that does not know the
// question starts up instead — a wrapper, a shim, a test's stand-in — and its
// hooks fire: a SessionStart naming a conversation of its own was recorded
// as the session's, and a restore resumed that conversation, or none. Under
// the mark, `reminal hook` writes nothing, and Enclosing names no session.
const HookProbeEnv = "REMINAL_HOOK_PROBE"

// ProbeEnv is environ for a probe: every REMINAL_ variable left out (the
// session it would name is not its own) and the mark set.
func ProbeEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "REMINAL_") {
			out = append(out, kv)
		}
	}
	return append(out, HookProbeEnv+"=1")
}

// ProbeCommand is a probe of name with args: its environment ProbeEnv's, no
// input (it reads none: os/exec's null device), ended with ctx.
func ProbeCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = ProbeEnv(os.Environ())
	return cmd
}

// InProbe says whether this process runs under a probe's mark.
func InProbe() bool { return os.Getenv(HookProbeEnv) != "" }

// probeMarked says whether pid was started under a probe's mark: what a
// process started from it carries even where it scrubs its own environment
// (a harness's hooks and helpers may be started so). False where it cannot
// be read.
func probeMarked(pid int) bool {
	for _, kv := range processEnv(pid) {
		if strings.HasPrefix(kv, HookProbeEnv+"=") && kv != HookProbeEnv+"=" {
			return true
		}
	}
	return false
}
