// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux || darwin

package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reminal/internal/session"
)

// An agent's --help is asked for as a probe: a program that does not know
// --help starts up instead, a child of the restoring session's reminal, and
// its hooks must name no session — so it gets no REMINAL_ variable and the
// probe's mark (session.ProbeCommand), and the plain-output ones as before.
func TestAnAgentsHelpIsAskedAsAProbe(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("REMINAL_SESSION", "AAAA2345")
	dir := t.TempDir()
	out := filepath.Join(dir, "env")
	agent := filepath.Join(dir, "fakeagent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nenv > '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentHelp(agent)
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the agent was not asked: %v", err)
	}
	env := "\n" + string(b)
	for _, want := range []string{session.HookProbeEnv + "=1", "NO_COLOR=1", "TERM=dumb", "CI=1"} {
		if !strings.Contains(env, "\n"+want+"\n") {
			t.Errorf("asked without %s", want)
		}
	}
	if strings.Contains(env, "\nREMINAL_SESSION=") {
		t.Error("asked with the session's REMINAL_SESSION")
	}
}

// reminalForHooks is reminal built from this tree, for a fake agent to fire
// its hooks through; skipped where no Go toolchain is on PATH.
func reminalForHooks(t *testing.T) string {
	t.Helper()
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go on PATH to build reminal with")
	}
	bin := filepath.Join(t.TempDir(), "reminal")
	cmd := exec.Command(gobin, "build", "-o", bin, "reminal/cmd/reminal")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building reminal: %v\n%s", err, out)
	}
	return bin
}

// A restoring session asks its agent's --help; an agent that does not know
// --help starts up and fires its SessionStart with a conversation of its
// own (the restore suite's stand-in does). The session's conversation, its
// written list, its agent's ready and its hook state are left alone — with
// the probe's mark kept, and scrubbed by the agent before its hooks run.
func TestAnAgentsHelpProbeLeavesTheSessionAlone(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	rem := reminalForHooks(t)
	for _, scrub := range []bool{false, true} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		const id, own, phantom = "HELP2345", "0f6c2a4e-1b7d-4c39-9e21-5a8b3c7d9e10", "9d1e7c55-2a3b-4f6e-8c9d-0e1f2a3b4c5d"
		// This process is the session's reminal: the agent asked is its child.
		if err := session.WriteActive(session.Active{ID: id, PID: os.Getpid()}); err != nil {
			t.Fatal(err)
		}
		if err := session.WriteConv(id, own); err != nil {
			t.Fatal(err)
		}
		env := "REMINAL_KEYSTORE=file"
		if scrub {
			env = "env -u " + session.HookProbeEnv + " " + env
		}
		agent := filepath.Join(t.TempDir(), "fakeagent")
		script := "#!/bin/sh\n"
		for _, verb := range []string{"done", "ready"} {
			script += "printf '{\"session_id\":\"" + phantom + "\",\"source\":\"resume\"}' | " + env + " '" + rem + "' hook " + verb + "\n"
		}
		if err := os.WriteFile(agent, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		agentHelp(agent)
		if at := session.HookReadyAt(id); !at.IsZero() {
			t.Fatalf("scrub=%v: the agent asked its --help marked the session's agent ready at %v", scrub, at)
		}
		if hs := session.ReadHookStateAnyAge(id); hs != nil {
			t.Fatalf("scrub=%v: the agent asked its --help wrote the session's hook state: %+v", scrub, *hs)
		}
		if got := session.ReadConv(id); got != own {
			t.Fatalf("scrub=%v: its conversation was recorded as the session's: %q, want %q", scrub, got, own)
		}
		if session.ConvWritten(id, phantom) {
			t.Fatalf("scrub=%v: its conversation was marked written for the session", scrub)
		}
		_ = session.ClearActive(id)
	}
}
