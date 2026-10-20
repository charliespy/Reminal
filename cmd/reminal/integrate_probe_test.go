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

// integrate registers itself through an agent's own CLI (`<agent> mcp add`)
// as a probe: no REMINAL_ variable, the probe's mark — an agent that does
// not know the subcommand starts up instead, and its hooks are no session's.
func TestAnAgentsCLIIsRunAsAProbe(t *testing.T) {
	t.Setenv("REMINAL_SESSION", "AAAA2345")
	dir := t.TempDir()
	out := filepath.Join(dir, "env")
	agent := filepath.Join(dir, "fakeagent")
	if err := os.WriteFile(agent, []byte("#!/bin/sh\nenv > '"+out+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	step := planStep{target: agentTarget{Name: "fake", cliAdd: []string{"mcp", "add", "reminal", "%CMD%"}}, binPath: agent}
	if err := applyViaCLI(step, "/bin/reminal", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the agent's CLI was not run: %v", err)
	}
	env := "\n" + string(b)
	if !strings.Contains(env, "\n"+session.HookProbeEnv+"=1\n") {
		t.Error("run without the probe's mark")
	}
	if strings.Contains(env, "\nREMINAL_SESSION=") {
		t.Error("run with the session's REMINAL_SESSION")
	}
}
