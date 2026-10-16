// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build windows

package client

import (
	"os"
	"syscall"
	"testing"
)

// handshakeFDOf: see handshakefd_unix_test.go. A handle of its own.
func handshakeFDOf(t *testing.T, w *os.File) int {
	t.Helper()
	p := syscall.Handle(0)
	cur, _ := syscall.GetCurrentProcess()
	if err := syscall.DuplicateHandle(cur, syscall.Handle(w.Fd()), cur, &p, 0, false, syscall.DUPLICATE_SAME_ACCESS); err != nil {
		t.Fatal(err)
	}
	w.Close()
	return int(p)
}
