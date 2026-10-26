// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package updater

import "syscall"

// startInPlace replaces this process with bin: same pid, same terminal.
func startInPlace(bin string, args, env []string) error {
	return syscall.Exec(bin, args, env)
}
