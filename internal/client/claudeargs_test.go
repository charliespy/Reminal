// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/json"
	"os"
	"testing"

	"reminal/internal/session"
)

// What a restore types for claudes started with a system prompt, for
// scripts/restore-test/claude-resume-args.sh to have the real claude parse
// (REMINAL_CLAUDE_ARGS names the file).
func TestClaudeResumeArgsForTheRig(t *testing.T) {
	out := os.Getenv("REMINAL_CLAUDE_ARGS")
	if out == "" {
		t.Skip("run by scripts/restore-test/claude-resume-args.sh")
	}
	useHelp(t)
	conv := "0a3d3e8e-1111-4222-8333-944455556666"
	type line struct{ Started, Typed string }
	var lines []line
	for _, args := range [][]string{
		{"claude", "--append-system-prompt", "be brief"},
		{"claude", "--system-prompt", "be brief", "--model", "opus"},
		{"claude", "--dangerously-skip-permissions", "--append-system-prompt", "be brief"},
	} {
		r := session.Restore{ID: "A", Fg: "claude", Cwd: "/w", Conv: conv, FgArgs: args}
		argv, _ := resumePlan(r, []session.Restore{r})
		lines = append(lines, line{shellJoin(args), shellJoin(argv)})
	}
	b, _ := json.Marshal(lines)
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
