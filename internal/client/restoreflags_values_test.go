// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"reminal/internal/session"
)

// claude's --help names --system-prompt and --append-system-prompt again
// inside another flag's description, wrapped onto lines of their own
// ("--system-prompt or", "--append-system-prompt included — sent, …"). Read
// as entries, they made both look like flags that take no value: a resume
// kept them bare, and the bare flag took --resume as its value, so the
// restored claude started a new conversation.
func TestClaudeHelpPromptFlagsTakeAValue(t *testing.T) {
	b, err := os.ReadFile("testdata/help/claude.txt")
	if err != nil {
		t.Fatal(err)
	}
	spec := parseHelpFlags(string(b))
	for _, f := range []string{"--append-system-prompt", "--system-prompt", "--model", "--add-dir"} {
		if takes, ok := spec[f]; !ok || !takes {
			t.Errorf("%s: listed %v, takes a value %v; want a flag that takes a value", f, ok, takes)
		}
	}
	if takes := spec["--dangerously-skip-permissions"]; takes {
		t.Error("--dangerously-skip-permissions read as taking a value")
	}
}

// A flag's own entry wins over anything after it: a mention indented as far
// as the descriptions is a description, and a later entry of the same name
// does not replace the first.
func TestParseHelpFlagsKeepsAFlagsOwnEntry(t *testing.T) {
	help := `Options:
  --prompt <text>          The prompt to use
  --other                  Something else; see
                           --prompt or
                           --prompt-file for more
  --prompt                 a second entry, later
`
	spec := parseHelpFlags(help)
	if !spec["--prompt"] {
		t.Fatalf("--prompt read as taking no value: %v", spec)
	}
	if _, ok := spec["--prompt-file"]; ok {
		t.Fatalf("a mention inside a description read as an entry: %v", spec)
	}
	if takes, ok := spec["--other"]; !ok || takes {
		t.Fatalf("--other: %v", spec)
	}
}

// A claude started with a system prompt comes back with it, on its own
// conversation.
func TestResumeKeepsTheSystemPrompt(t *testing.T) {
	useHelp(t)
	for _, f := range []string{"--append-system-prompt", "--system-prompt"} {
		r := session.Restore{ID: "A", Fg: "claude", Cwd: "/w", Conv: "f19308b1-5c22-4b55-96d9-e1803b9501b6",
			FgArgs: []string{"claude", f, "be brief", "--model", "opus"}}
		argv, note := resumePlan(r, []session.Restore{r})
		want := []string{"claude", f, "be brief", "--model", "opus", "--resume", r.Conv}
		if !reflect.DeepEqual(argv, want) || note != "" {
			t.Errorf("%s: got %q %q, want %q", f, argv, note, want)
		}
	}
}

// A flag that takes a value, with none of its own, is left out — never kept
// bare where it would take the resume's --resume as its value — and the
// note says so.
func TestResumeLeavesOutAValueFlagWithoutAValue(t *testing.T) {
	useHelp(t)
	for _, args := range [][]string{
		{"claude", "--dangerously-skip-permissions", "--model"}, // last
		{"claude", "--model", "--dangerously-skip-permissions"}, // another flag follows
	} {
		r := session.Restore{ID: "A", Fg: "claude", Cwd: "/w", Conv: "f19308b1-5c22-4b55-96d9-e1803b9501b6", FgArgs: args}
		argv, note := resumePlan(r, []session.Restore{r})
		want := []string{"claude", "--dangerously-skip-permissions", "--resume", r.Conv}
		if !reflect.DeepEqual(argv, want) || !strings.Contains(note, "--model (no value of its own)") {
			t.Errorf("%q: got %q %q, want %q and a note", args, argv, note, want)
		}
	}
}
