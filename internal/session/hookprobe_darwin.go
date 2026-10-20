// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build darwin

package session

import (
	"bytes"
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// processEnv is the environment pid was started with, from the kernel
// (KERN_PROCARGS2: argc, the executable path, NUL padding, argc arguments,
// then the environment, each NUL-terminated, up to an empty string); nil
// when it cannot be read.
func processEnv(pid int) []string {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(b) < 4 {
		return nil
	}
	argc := int(binary.LittleEndian.Uint32(b[:4]))
	rest := b[4:]
	i := bytes.IndexByte(rest, 0) // past the executable path
	if i < 0 {
		return nil
	}
	rest = rest[i:]
	for len(rest) > 0 && rest[0] == 0 { // and its padding
		rest = rest[1:]
	}
	for n := 0; n < argc; n++ { // past the arguments
		j := bytes.IndexByte(rest, 0)
		if j < 0 {
			return nil
		}
		rest = rest[j+1:]
	}
	var env []string
	for len(rest) > 0 {
		j := bytes.IndexByte(rest, 0)
		if j <= 0 {
			break // its end
		}
		env = append(env, string(rest[:j]))
		rest = rest[j+1:]
	}
	return env
}
