// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"os"
	"syscall"
	"testing"
)

// handshakeFDOf gives a Tunnel the write end of a test's pipe as an fd it
// owns, the way a spawned forward owns the one it inherits: handshakeWriter
// wraps the number in a file of its own and closes it. Passing w.Fd() itself
// left w a second owner of that number, and w's finaliser closed it again,
// after some other test had opened a socket or file on it.
func handshakeFDOf(t *testing.T, w *os.File) int {
	t.Helper()
	fd, err := syscall.Dup(int(w.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	return fd
}
