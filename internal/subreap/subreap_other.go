// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !linux

package subreap

import (
	"errors"
	"os"
	"os/exec"
)

// Run elsewhere runs the command and returns its exit code: only Linux has
// child subreapers; elsewhere the system's own init collects orphans.
func Run(args []string) (int, error) {
	argv, err := command(args)
	if err != nil {
		return 2, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	if err != nil {
		return 127, err
	}
	return 0, nil
}
