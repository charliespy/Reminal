// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package session

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// processEnv is the environment pid was started with (/proc/<pid>/environ);
// nil when it cannot be read.
func processEnv(pid int) []string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
}

// processSession is the session pid is in (its leader's pid); 0 when it
// cannot be read.
func processSession(pid int) int {
	sid, err := unix.Getsid(pid)
	if err != nil {
		return 0
	}
	return sid
}

// probeIdent is what tells the process at pid from a later one given the
// same pid: when it started, in clock ticks since boot.
func probeIdent(pid int) (string, bool) {
	t, ok := procStartTicks(pid)
	if !ok {
		return "", false
	}
	return strconv.FormatInt(t, 10), true
}
