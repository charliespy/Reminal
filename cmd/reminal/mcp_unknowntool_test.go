// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"runtime"
	"strings"
	"testing"
)

// A tool name the server does not have is answered as such on every OS; only
// the window-notes tools themselves are macOS-only.
func TestMCPUnknownToolIsUnknownOnEveryOS(t *testing.T) {
	s := &mcpServer{}
	_, err := s.callTool("no_such_tool", nil)
	if err == nil || err.Error() != "unknown tool: no_such_tool" {
		t.Fatalf("an unknown tool answered %v, want \"unknown tool: no_such_tool\"", err)
	}
	if runtime.GOOS == "darwin" {
		return // the notes tools run here; nothing macOS-only to say
	}
	for name := range mcpMacOnlyTools {
		_, err := s.callTool(name, nil)
		if err == nil || !strings.Contains(err.Error(), "macOS-only") {
			t.Errorf("%s answered %v, want the macOS-only answer", name, err)
		}
	}
}
