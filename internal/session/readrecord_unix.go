// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package session

import (
	"os"
	"syscall"
)

// openRecord opens path to read: never a link at its name (O_NOFOLLOW), and
// never waiting for a writer (O_NONBLOCK: a FIFO opens at once, and is then
// refused as no regular file).
func openRecord(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
