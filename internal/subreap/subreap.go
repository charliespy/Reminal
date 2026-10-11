// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// Package subreap runs a command as the parent of everything it starts.
//
// A process whose parent ends before it is handed to the nearest ancestor
// that has asked for them (a "child subreaper", Linux), else to pid 1; when
// it ends, that ancestor must collect it, or it stays a zombie. Some programs
// leave such orphans as a matter of course (a browser's crash reporter and
// zygote children, a shell's background job that outlives it), and where
// pid 1 does not collect them (a container whose pid 1 is not an init) they
// stay zombies for good.
//
// `reminal subreap -- cmd args…` is that ancestor for cmd's tree: it starts
// cmd, takes every orphan of the tree as its own, collects each the moment
// it ends, and exits — with cmd's own status — once the last of them has.
// Nothing of the tree reaches pid 1.
package subreap

import "fmt"

// usage is said for arguments that are not "-- cmd args…".
func usage() error { return fmt.Errorf("usage: reminal subreap -- command [args…]") }

// command takes the command from the arguments after "subreap".
func command(args []string) ([]string, error) {
	if len(args) < 2 || args[0] != "--" {
		return nil, usage()
	}
	return args[1:], nil
}
