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
	const conv = "51580c55-63f5-43c1-bbea-4d0425f09835"
	files := map[string]string{
		"claude": filepath.Join(home, ".claude", "projects", "-work", conv+".jsonl"),
		"qwen":   filepath.Join(home, ".qwen", "projects", "-work", "chats", conv+".jsonl"),
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
	if len(files) != len(convStores) {
		t.Errorf("convStores has %d agents, this test %d", len(convStores), len(files))
	}
	if sleepsExactly("gemini") || convSize("gemini", conv) != 0 {
		t.Error("gemini, which resumes only the latest, sleeps")
	}
}
