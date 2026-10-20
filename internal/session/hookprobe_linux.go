// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package session

import (
	"os"
	"strconv"
	"strings"
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
