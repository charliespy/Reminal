// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !darwin && !windows

package client

import (
	"os"
	"runtime"
	"strconv"
)

// processExecPath is the file pid's process was started from: Linux's
// /proc/<pid>/exe; "" elsewhere, or if not read.
func processExecPath(pid int) string {
	if runtime.GOOS != "linux" {
		return ""
	}
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return ""
	}
	return p
}

// processArgsNative: Linux reads /proc directly (see processArgs); others
// have only ps.
func processArgsNative(int) ([]string, bool) { return nil, false }
