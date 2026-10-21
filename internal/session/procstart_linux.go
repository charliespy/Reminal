// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package session

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// procStartTime returns when the process now at pid was started, derived from
// /proc/<pid>/stat field 22 (starttime, in clock ticks since boot) plus the
// system boot time from /proc/stat's "btime". Used to detect PID reuse — see the
// darwin twin for the contract. Best-effort: (zero, false) on any parse error.
//
// USER_HZ is assumed to be 100 (near-universal on Linux). An off value only
// scales the computed uptime; because reuse is detected with a wide tolerance
// and a recycled PID's process is always far newer than the stale record, the
// approximation can't misclassify a live session as reused.
func procStartTime(pid int) (time.Time, bool) {
	ticks, ok := procStartTicks(pid)
	if !ok {
		return time.Time{}, false
	}
	btime, ok := bootTimeUnix()
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(btime+ticks/UserHZ, 0), true
}

// UserHZ is the clock tick /proc counts in (USER_HZ): 100 on Linux.
const UserHZ = 100

// procRoot is where /proc is read from (a test sets its own).
var procRoot = "/proc"

// procStartTicks is /proc/<pid>/stat field 22: when the process started, in
// clock ticks since boot — the kernel's own count, which no clock step moves.
func procStartTicks(pid int) (int64, bool) {
	b, err := os.ReadFile(procRoot + "/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	return StatStartTicks(b)
}

// StatStartTicks is field 22 of a /proc/<pid>/stat's contents (starttime):
// when that process started, in clock ticks (UserHZ) since boot.
func StatStartTicks(b []byte) (int64, bool) {
	s := string(b)
	// comm (field 2) is parenthesized and may contain spaces or ')', so scan
	// past the LAST ')': the fields after it begin at state (field 3).
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, false
	}
	fields := strings.Fields(s[i+2:])
	const starttimeIdx = 19 // field 22 minus the 3 fields consumed before state
	if len(fields) <= starttimeIdx {
		return 0, false
	}
	ticks, err := strconv.ParseInt(fields[starttimeIdx], 10, 64)
	if err != nil {
		return 0, false
	}
	return ticks, true
}

// procIdentity names the process now at pid by its boot and its start in
// that boot's ticks: the same identity is the same process, another is not,
// and no clock is read. /proc/stat's btime is the wall clock minus uptime,
// read anew each time, so a start time made of it moves with every clock
// step either way (NTP catching up a paused VM or Docker host): a live
// process read as started later, and taken for another. ("", false) when
// either cannot be read.
func procIdentity(pid int) (string, bool) {
	ticks, ok := procStartTicks(pid)
	if !ok {
		return "", false
	}
	b, err := os.ReadFile(procRoot + "/sys/kernel/random/boot_id")
	if err != nil {
		return "", false
	}
	boot := strings.TrimSpace(string(b))
	if boot == "" {
		return "", false
	}
	return boot + ":" + strconv.FormatInt(ticks, 10), true
}

func bootTimeUnix() (int64, bool) {
	b, err := os.ReadFile(procRoot + "/stat")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return 0, false
			}
			return n, true
		}
	}
	return 0, false
}
