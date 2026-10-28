// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package pty

import (
	"os"
	"strconv"
	"strings"
)

// procMark names the process now at pid by when it started, in ticks since
// boot (/proc/<pid>/stat's starttime: no clock read, so no clock step moves
// it), with its parent's pid. ok false when it cannot be read: pid gone.
func procMark(pid int) (mark string, ppid int, ok bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", 0, false
	}
	// After the command's ")" (which may hold spaces and parentheses):
	// state, ppid, … starttime is the 20th field from there.
	i := strings.LastIndexByte(string(b), ')')
	if i < 0 {
		return "", 0, false
	}
	f := strings.Fields(string(b[i+1:]))
	if len(f) < 20 {
		return "", 0, false
	}
	ppid, err = strconv.Atoi(f[1])
	if err != nil {
		return "", 0, false
	}
	return f[19], ppid, true
}
