// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !linux && !windows

package pty

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// startShell starts the command newCmd builds on a new PTY. ShellCgroupEnv is
// Linux's, and ignored here.
func startShell(newCmd func() *exec.Cmd) (*os.File, *exec.Cmd, *ShellCgroup, error) {
	cmd := newCmd()
	ptmx, err := pty.Start(cmd)
	return ptmx, cmd, nil, err
}
