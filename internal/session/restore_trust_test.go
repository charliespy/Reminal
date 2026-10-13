// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A session's conversation id is typed into a shell when it is restored, so
// only an id is ever read back — whatever wrote the file — and only an id is
// written.
func TestConvIsOnlyEverAnID(t *testing.T) {
	dir := isolateHome(t)
	if err := WriteConv("ABCD2345", "f19308b1-5c22-4b55-96d9-e1803b9501b6"); err != nil {
		t.Fatal(err)
	}
	if got := ReadConv("ABCD2345"); got != "f19308b1-5c22-4b55-96d9-e1803b9501b6" {
		t.Fatalf("an id read back as %q", got)
	}
	if err := WriteConv("ABCD2345", "x\x15y"); err == nil {
		t.Fatal("wrote a conversation id holding a control character")
	}
	// Written by something else: not an id, so none.
	for _, bad := range []string{"abc\x1b[201~\x15defghij", "has spaces in it", "x'; y", "short"} {
		if err := os.WriteFile(filepath.Join(dir, "restore", "ABCD2345.conv"), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, at := ReadConvAt("ABCD2345"); got != "" || !at.IsZero() {
			t.Errorf("%q read back as %q", bad, got)
		}
	}
}

// A plain record beside a sealed one is never read — anything that can write
// the folder could put words in the session's mouth without the key — and is
// removed; alone, it is sealed at once.
func TestPlainRecordBesideASealedOneIsNotRead(t *testing.T) {
	dir := isolateHome(t)
	rd := filepath.Join(dir, "restore")
	if err := WriteRestore(Restore{ID: "ABCD2345", PIN: "424242", Fg: "claude", Conv: "f19308b1-5c22-4b55-96d9-e1803b9501b6", SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	planted, _ := json.Marshal(Restore{ID: "ABCD2345", PIN: "999999", Fg: "claude", FgArgs: []string{"claude", "--model", "planted"}, SavedAt: time.Now()})
	lp := filepath.Join(rd, "ABCD2345.json")
	if err := os.WriteFile(lp, planted, 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	_ = os.Chtimes(lp, later, later) // newer than the sealed one
	r, err := ReadRestore("ABCD2345")
	if err != nil {
		t.Fatal(err)
	}
	if r.PIN != "424242" || len(r.FgArgs) != 0 {
		t.Fatalf("read the plain record beside the sealed one: %+v", r)
	}
	if _, err := os.Stat(lp); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the plain record beside the sealed one is still there")
	}
}

// A plain record named for one session but written for another is not that
// session's.
func TestPlainRecordForAnotherSessionIsNotRead(t *testing.T) {
	dir := isolateHome(t)
	rd := filepath.Join(dir, "restore")
	_ = os.MkdirAll(rd, 0o700)
	other, _ := json.Marshal(Restore{ID: "WXYZ6789", PIN: "135791", SavedAt: time.Now()})
	if err := os.WriteFile(filepath.Join(rd, "ABCD2345.json"), other, 0o600); err != nil {
		t.Fatal(err)
	}
	if r, err := ReadRestore("ABCD2345"); err == nil {
		t.Fatalf("read another session's plain record as ABCD2345's: %+v", r)
	}
}
