// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build windows

package updater

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
)

// startInPlace runs bin in this console and exits with its status: Windows
// has no exec, so this process stays as its parent until it ends.
func startInPlace(bin string, args, env []string) error {
	cmd := exec.Command(bin, args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return err
	}
	// A Ctrl-C reaches every process in the console; it is the new one's to
	// handle, and this one ending would hand the console back to the shell
	// while the new one still runs in it.
	signal.Ignore(os.Interrupt)
	err := cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
	return nil
}
