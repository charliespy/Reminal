// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package pty

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// A write nothing takes (a pipe nobody reads, as a PTY whose input is full)
// lets go of its caller once stop closes; one that is taken is written.
func TestAWriteNothingTakesIsLetGoOnStop(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	stop := make(chan struct{})
	got := make(chan error, 1)
	go func() { _, err := writeCancel(w, []byte("held"), stop); got <- err }()
	select {
	case err := <-got:
		t.Fatalf("returned before anything took it or stop: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(stop)
	select {
	case err := <-got:
		if !errors.Is(err, errWriteStopped) {
			t.Fatalf("let go with %v, want errWriteStopped", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not let go of a write nothing takes")
	}
	var buf bytes.Buffer
	if n, err := writeCancel(&buf, []byte("taken"), make(chan struct{})); n != 5 || err != nil || buf.String() != "taken" {
		t.Fatalf("a write taken: %d %v %q", n, err, buf.String())
	}
	if _, err := writeCancel(&buf, []byte("x"), stop); !errors.Is(err, errWriteStopped) {
		t.Fatalf("a write asked after stop: %v", err)
	}
}
