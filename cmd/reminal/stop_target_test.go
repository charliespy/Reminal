// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"testing"

	"reminal/internal/session"
)

// `reminal stop <id> --yes` stops that id or nothing: a forward that has
// already gone must not be matched, by name or prefix, to a shell session.
func TestStopYesTakesOnlyAnExactID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows' home
	t.Setenv("REMINAL_SESSION", "")
	writeRec(t, session.Active{ID: "AAAA1111", PIN: "1", Name: "build"})
	writeRec(t, session.Active{ID: "BBBB2222", PIN: "3", Kind: session.KindPort, Port: 3000})

	if a, err := resolveStopTarget("ZZZZ9999", true); err == nil {
		t.Fatalf("an id that isn't there resolved to %s", a.ID)
	}
	writeRec(t, session.Active{ID: "CCCC3333", PIN: "4", Name: "ZZZZ9999"})
	if a, err := resolveStopTarget("ZZZZ9999", true); err == nil {
		t.Fatalf("--yes with a gone id stopped %s, matched by name", a.ID)
	}
	if a, err := resolveStopTarget("bbbb2222", true); err != nil || a.ID != "BBBB2222" {
		t.Fatalf("the exact id didn't resolve: %v %v", a, err)
	}
	// Without --yes, and for what doesn't look like an id, the usual matching.
	if a, err := resolveStopTarget("ZZZZ9999", false); err != nil || a.ID != "CCCC3333" {
		t.Fatalf("without --yes, the name no longer matched: %v %v", a, err)
	}
	if a, err := resolveStopTarget("3000", true); err != nil || a.ID != "BBBB2222" {
		t.Fatalf("a port number no longer resolved: %v %v", a, err)
	}
}
