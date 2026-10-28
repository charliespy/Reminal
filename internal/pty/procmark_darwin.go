// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package pty

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// procMark names the process now at pid by when it started, as the kernel
// keeps it (fixed at its start), with its parent's pid. ok false when it
// cannot be read: pid gone.
func procMark(pid int) (mark string, ppid int, ok bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil || kp.Proc.P_pid != int32(pid) {
		return "", 0, false
	}
	tv := kp.Proc.P_starttime
	return strconv.FormatInt(int64(tv.Sec), 10) + "." + strconv.FormatInt(int64(tv.Usec), 10), int(kp.Eproc.Ppid), true
}
