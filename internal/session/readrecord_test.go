// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A session's records are read by whoever asks after it, and the folder
// they are in may be written by someone else (a machine whose supervisor
// reads its user's): a FIFO at a record's name never holds the read (it
// waited for a writer for ever), and a link at its name or a huge file is
// not read.
func TestARecordIsReadOnlyIfItIsAFileOfItsOwn(t *testing.T) {
	const id = "FIFOREC1"
	ap, err := activePath(id)
	if err != nil {
		t.Fatal(err)
	}
	rp, err := restorePath(id, ".sealed")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{ap, rp} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Mkfifo(p, 0o600); err != nil {
			t.Skip("no FIFOs here:", err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(ap); _ = os.Remove(rp) })
	within := func(what string, f func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- f() }()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: a FIFO read as a record", what)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: held by a FIFO at the record's name", what)
		}
	}
	within("ReadActiveByID", func() error { _, err := ReadActiveByID(id); return err })
	within("ReadRestore", func() error { _, err := ReadRestore(id); return err })

	// A link at the name, to a record elsewhere: not read.
	_ = os.Remove(ap)
	other := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(other, []byte(`{"id":"`+id+`","pid":`+strconv.Itoa(os.Getpid())+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, ap); err != nil {
		t.Fatal(err)
	}
	if a, err := ReadActiveByID(id); err == nil {
		t.Errorf("a record read through a link at its name: %+v", a)
	}
	// One past what a record can be: not read.
	_ = os.Remove(ap)
	if err := os.WriteFile(ap, []byte(`{"id":"`+id+`","title":"`+strings.Repeat("x", maxActiveRecord)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadActiveByID(id); err == nil {
		t.Error("a record past its size read")
	}
}
