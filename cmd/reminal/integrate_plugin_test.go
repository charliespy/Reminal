// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reminal's plugin is written, rewritten and removed; a file of the same
// name that is not reminal's is never touched.
func TestApplyPlugin(t *testing.T) {
	home := t.TempDir()
	p := &pluginFile{path: ".config/opencode/plugin/reminal.js", body: opencodePlugin}
	path := filepath.Join(home, p.path)
	if err := applyPlugin(p, home, "/opt/reminal", false); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(b), pluginMark) || !strings.Contains(string(b), `"/opt/reminal"`) {
		t.Fatalf("written: %v\n%s", err, b)
	}
	if err := applyPlugin(p, home, "/usr/bin/reminal", false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"/usr/bin/reminal"`) {
		t.Fatal("not rewritten for where reminal is now")
	}
	if err := applyPlugin(p, home, "", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
	if err := applyPlugin(p, home, "", true); err != nil {
		t.Fatalf("removing what is not there: %v", err)
	}
	if err := os.WriteFile(path, []byte("// someone's own\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if applyPlugin(p, home, "/opt/reminal", false) == nil || applyPlugin(p, home, "", true) == nil {
		t.Fatal("a plugin that is not reminal's was taken over")
	}
	if b, _ := os.ReadFile(path); string(b) != "// someone's own\n" {
		t.Fatal("a plugin that is not reminal's was changed")
	}
}
