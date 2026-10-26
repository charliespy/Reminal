// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// A file saved into a folder lands in that folder: a link already there
// under its name, pointing at a file not yet made, is never followed.
func TestASavedFileNeverFollowsALinkInTheFolder(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	aim := filepath.Join(outside, "made-elsewhere")
	if err := os.Symlink(aim, filepath.Join(dir, "report.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := writeRendezvousFile(dir, "report.txt", []byte("pasted"))
	if _, serr := os.Lstat(aim); serr == nil {
		t.Fatalf("the paste followed the link and made %s (saved as %q, err %v)", aim, got, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("saved at %q, outside %q", got, dir)
	}
	if b, err := os.ReadFile(got); err != nil || string(b) != "pasted" {
		t.Fatalf("saved %q: %q %v", got, b, err)
	}
}
