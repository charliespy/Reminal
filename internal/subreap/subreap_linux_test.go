// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package subreap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// children are this process's children still there, zombies included.
func children(t *testing.T) []string {
	t.Helper()
	var out []string
	tasks, _ := filepath.Glob("/proc/self/task/*/children")
	for _, f := range tasks {
		b, _ := os.ReadFile(f)
		out = append(out, strings.Fields(string(b))...)
	}
	return out
}

// A command that leaves a process behind and ends: Run collects that one too,
// when it ends, and returns the command's own status — nothing left over.
func TestRunCollectsWhatTheCommandLeaves(t *testing.T) {
	for _, tc := range []struct{ name, script string }{
		{"a background job", "sleep 0.3 & exit 3"},
		{"one in a session of its own", "setsid sleep 0.3 & exit 3"},
		{"a grandchild", "sh -c 'sleep 0.3 &' ; exit 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t0 := time.Now()
			code, err := Run([]string{"--", "sh", "-c", tc.script})
			if err != nil || code != 3 {
				t.Fatalf("code %d err %v, want 3", code, err)
			}
			if d := time.Since(t0); d < 250*time.Millisecond {
				t.Fatalf("returned in %v: before what the command left had ended", d)
			}
			if c := children(t); len(c) > 0 {
				t.Fatalf("left behind: %v", c)
			}
		})
	}
}

func TestRunSignalStatus(t *testing.T) {
	code, err := Run([]string{"--", "sh", "-c", "kill -TERM $$"})
	if err != nil || code != 128+15 {
		t.Fatalf("code %d err %v, want 143", code, err)
	}
}

func TestRunUsage(t *testing.T) {
	if code, err := Run(nil); err == nil || code != 2 {
		t.Fatalf("no command: code %d err %v", code, err)
	}
	if code, err := Run([]string{"sh"}); err == nil || code != 2 {
		t.Fatalf("no --: code %d err %v", code, err)
	}
	if code, err := Run([]string{"--", "no-such-command-here"}); err == nil || code != 127 {
		t.Fatalf("missing command: code %d err %v", code, err)
	}
}
