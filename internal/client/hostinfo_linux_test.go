// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// hostProcFiles sets a /proc of its own: its uptime, and PID 1's stat (none
// when stat is "").
func hostProcFiles(t *testing.T, uptime, stat string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "uptime"), []byte(uptime), 0o644); err != nil {
		t.Fatal(err)
	}
	if stat != "" {
		if err := os.MkdirAll(filepath.Join(root, "1"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "1", "stat"), []byte(stat), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := hostProc
	hostProc = root
	t.Cleanup(func() { hostProc = old })
}

// pid1Stat is a /proc/1/stat whose process started at ticks after boot, its
// name with a space and a ")" in it, as a comm may be.
func pid1Stat(ticks string) string {
	return "1 (vm entry) x) S 0 1 1 0 -1 4194560 1000 0 0 0 5 3 0 0 20 0 1 0 " + ticks + " 4321792 900 18446744073709551615 1 1 0 0 0 0 0 4096 0 0 0 0 17 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n"
}

// In a container, /proc/uptime is the host kernel's: up since its PID 1
// started is the container's own age.
func TestUptimeIsSinceItsPID1Started(t *testing.T) {
	hostProcFiles(t, "36000.50 70000.00\n", pid1Stat("3599050")) // PID 1 at 35990.50 s after the host's boot
	var h HostInfo
	linuxUptime(&h)
	if h.Uptime != 10 {
		t.Fatalf("uptime %d, want 10 (the host up 36000 s, its PID 1 started 10 s ago)", h.Uptime)
	}
}

// On a machine of its own, PID 1 starts at boot: about /proc/uptime.
func TestUptimeOnAMachineOfItsOwnIsAboutItsKernels(t *testing.T) {
	hostProcFiles(t, "36000.50 70000.00\n", pid1Stat("150"))
	var h HostInfo
	linuxUptime(&h)
	if h.Uptime != 35999 {
		t.Fatalf("uptime %d, want 35999", h.Uptime)
	}
}

// No PID 1 to read, or one that is not a stat (or started after now):
// /proc/uptime as it is.
func TestUptimeWithNoPID1ToReadIsItsKernels(t *testing.T) {
	for name, stat := range map[string]string{"none": "", "not a stat": "garbage", "later than now": pid1Stat("9999999")} {
		hostProcFiles(t, "36000.50 70000.00\n", stat)
		var h HostInfo
		linuxUptime(&h)
		if h.Uptime != 36000 {
			t.Errorf("%s: uptime %d, want 36000", name, h.Uptime)
		}
	}
}

// No /proc/uptime to read: no uptime said (0), whatever PID 1 says.
func TestUptimeWithNoProcUptimeIsNone(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "1", "stat"), []byte(pid1Stat("150")), 0o644); err != nil {
		t.Fatal(err)
	}
	old := hostProc
	hostProc = root
	t.Cleanup(func() { hostProc = old })
	h := HostInfo{}
	linuxUptime(&h)
	if h.Uptime != 0 {
		t.Fatalf("uptime %d with no /proc/uptime, want 0", h.Uptime)
	}
}
