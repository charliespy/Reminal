// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux || darwin

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reminal/internal/session"
)

// The test binary stands in for `reminal hook` when a fake agent fires one.
func init() {
	if os.Getenv("HOOK_PROBE_TEST_ACT") == "hook" && len(os.Args) > 2 {
		_ = runHook(os.Args[2:])
		os.Exit(0)
	}
}

// An agent asked its version that does not know --version starts up, and
// its hooks fire (verbs, in order) with a conversation of its own — inside
// the session by the process tree, since the session's reminal started it.
// Nothing of it may be the session's: not its conversation, not one marked
// written, not its hook state, not its agent's ready. scrub: how the agent
// starts its hooks — with the probe's mark (markKept), with it scrubbed from
// their environment as a harness may (markUnset), or its whole self started
// again with an environment of nothing (`exec env -i`: markCleared), the
// mark gone from every process from the probe's own down.
func probeLeavesSessionAlone(t *testing.T, scrub markScrub, verbs ...string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const id, own, phantom = "SESS2345", "0f6c2a4e-1b7d-4c39-9e21-5a8b3c7d9e10", "9d1e7c55-2a3b-4f6e-8c9d-0e1f2a3b4c5d"
	// This process is the session's reminal: the probe is its child.
	if err := session.WriteActive(session.Active{ID: id, PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	if err := session.WriteConv(id, own); err != nil {
		t.Fatal(err)
	}
	env := ""
	if scrub == markUnset {
		env = "env -u " + session.HookProbeEnv + " "
	}
	dir := t.TempDir()
	agent := filepath.Join(dir, "claude")
	hooks := ""
	for _, verb := range verbs {
		hooks += "printf '{\"session_id\":\"" + phantom + "\",\"source\":\"resume\",\"hook_event_name\":\"Notification\",\"error\":\"rate_limit\"}' | HOOK_PROBE_TEST_ACT=hook " + env + "'" + bin + "' hook " + verb + "\n"
	}
	hooks += "echo '2.1.77 (Claude Code)'\n"
	script := "#!/bin/sh\n[ \"$1\" = --version ] || exit 0\n"
	if scrub == markCleared {
		// The same process, started again with nothing in its environment.
		inner := filepath.Join(dir, "inner.sh")
		if err := os.WriteFile(inner, []byte(hooks), 0o644); err != nil {
			t.Fatal(err)
		}
		script += "exec env -i HOME='" + home + "' PATH=\"$PATH\" /bin/sh '" + inner + "'\n"
	} else {
		script += hooks
	}
	if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := agentVersion(agent); !strings.Contains(got, "(Claude Code)") {
		t.Fatalf("the agent's version not read: %q", got)
	}
	if at := session.HookReadyAt(id); !at.IsZero() {
		t.Fatalf("the probe's SessionStart marked the session's agent ready at %v", at)
	}
	if hs := session.ReadHookStateAnyAge(id); hs != nil {
		t.Fatalf("the probe wrote the session's hook state: %+v", *hs)
	}
	if got := session.ReadConv(id); got != own {
		t.Fatalf("the probe's conversation was recorded as the session's: %q, want %q", got, own)
	}
	if session.ConvWritten(id, phantom) {
		t.Fatal("the probe's conversation was marked written for the session")
	}
}

// How a probe's agent starts its hooks (probeLeavesSessionAlone).
type markScrub int

const (
	markKept markScrub = iota
	markUnset
	markCleared
)

func TestAnAgentsVersionProbeWritesNothingOfTheSessions(t *testing.T) {
	probeLeavesSessionAlone(t, markKept, "ready")
}

func TestAnAgentsVersionProbeWritesNothingThoughItsHooksScrubTheMark(t *testing.T) {
	probeLeavesSessionAlone(t, markUnset, "ready")
}

// Started again with an environment of nothing, the mark is in none of its
// processes: its session, recorded while it runs, still says it is a probe.
func TestAnAgentsVersionProbeWritesNothingThoughItClearsItsWholeEnvironment(t *testing.T) {
	probeLeavesSessionAlone(t, markCleared, "done", "ready")
}

// The stand-in's own order: its turn's end, then its SessionStart. A session
// still loading its resume, marked ready by that, was typed into while it
// loaded: its ready state is left as it was (none yet).
func TestAnAgentsVersionProbeLeavesTheSessionsReadyStateAlone(t *testing.T) {
	probeLeavesSessionAlone(t, markKept, "done", "ready")
	probeLeavesSessionAlone(t, markUnset, "done", "ready")
}

// Every verb a hook may be fired with, under the mark: none writes anything
// of the session's.
func TestAnAgentsVersionProbeFiresNoHookVerbForTheSession(t *testing.T) {
	verbs := []string{"working", "input", "done", "failed", "notify", "subagent", "schedule", "ready"}
	probeLeavesSessionAlone(t, markKept, verbs...)
	probeLeavesSessionAlone(t, markUnset, verbs...)
	probeLeavesSessionAlone(t, markCleared, verbs...)
}
