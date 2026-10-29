// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// Each agent that sleeps has its conversation's file found where it keeps
// it, by id alone; an agent whose files are not known never sleeps.
func TestConvStores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	const conv = "51580c55-63f5-43c1-bbea-4d0425f09835"
	files := map[string]string{
		"claude": filepath.Join(home, ".claude", "projects", "-work", conv+".jsonl"),
		"qwen":   filepath.Join(home, ".qwen", "projects", "-work", "chats", conv+".jsonl"),
		"pi":     filepath.Join(home, ".pi", "agent", "sessions", "--work--", "2026-10-29T17-51-38-000Z_"+conv+".jsonl"),
		"codex":  filepath.Join(home, ".codex", "sessions", "2026", "10", "29", "rollout-2026-10-29T17-51-38-"+conv+".jsonl"),
	}
	for agent, p := range files {
		if convSize(agent, conv) != 0 {
			t.Fatalf("%s: found before written", agent)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := convSize(agent, conv); got != 3 {
			t.Errorf("%s: size %d, want 3", agent, got)
		}
		if convSize(agent, "../"+conv) != 0 {
			t.Errorf("%s: a pattern read as an id", agent)
		}
		if !sleepsExactly(agent) {
			t.Errorf("%s does not sleep", agent)
		}
	}
	for agent, s := range convStores {
		if _, tested := files[agent]; s.file != nil && !tested {
			t.Errorf("%s keeps files this test does not look for", agent)
		}
	}
	if !sleepsExactly("opencode") {
		t.Error("opencode, asked for its conversations, does not sleep")
	}
	if sleepsExactly("gemini") || convSize("gemini", conv) != 0 {
		t.Error("gemini, which resumes only the latest, sleeps")
	}
}
