// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import "testing"

func TestParseProcStat(t *testing.T) {
	r, ok := parseProcStat("4242 (node (MCP) srv) S 100 4242 100 34816 4242 4194560")
	if !ok || r.pid != 4242 || r.ppid != 100 || r.pgid != 4242 || r.comm != "node (MCP) srv" || r.zombie {
		t.Fatalf("parsed %+v %v", r, ok)
	}
	if r, ok := parseProcStat("7 (sh) Z 1 7 7"); !ok || !r.zombie {
		t.Fatalf("zombie: %+v %v", r, ok)
	}
	if _, ok := parseProcStat("garbage"); ok {
		t.Fatal("parsed garbage")
	}
}

func TestParsePS(t *testing.T) {
	rows := parsePS("  100     1   100 Ss   /bin/zsh\n  200   100   200 S+   /Users/me/My Tools/claude\n  300   100   300 Z    sleep\n")
	if len(rows) != 3 || rows[1].comm != "claude" || rows[1].ppid != 100 || !rows[2].zombie {
		t.Fatalf("parsed %+v", rows)
	}
}
