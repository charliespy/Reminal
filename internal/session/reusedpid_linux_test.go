// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package session

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// fakeProc is a /proc of one process at pid: its start in ticks, the boot's
// id and its btime, each changeable.
type fakeProc struct {
	t    *testing.T
	root string
	pid  int
}

func newFakeProc(t *testing.T, pid int) *fakeProc {
	root := t.TempDir()
	old := procRoot
	procRoot = root
	t.Cleanup(func() { procRoot = old })
	_ = os.MkdirAll(filepath.Join(root, strconv.Itoa(pid)), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "sys", "kernel", "random"), 0o755)
	return &fakeProc{t, root, pid}
}

func (f *fakeProc) set(ticks int64, boot string, btime int64) {
	f.t.Helper()
	// comm with a space and a ')' in it, as the parser must survive.
	stat := strconv.Itoa(f.pid) + " (re) minal) S 1 1 1 0 -1 4194560 0 0 0 0 0 0 0 0 20 0 1 0 " + strconv.FormatInt(ticks, 10) + " 0 0\n"
	for p, b := range map[string]string{
		filepath.Join(f.root, strconv.Itoa(f.pid), "stat"):          stat,
		filepath.Join(f.root, "sys", "kernel", "random", "boot_id"): boot + "\n",
		filepath.Join(f.root, "stat"):                               "cpu  1 2 3\nbtime " + strconv.FormatInt(btime, 10) + "\n",
	} {
		if err := os.WriteFile(p, []byte(b), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// On Linux a process is named by its boot and its start in that boot's
// ticks, never by a clock: /proc/stat's btime is the wall clock minus uptime,
// so a clock stepped forward (NTP catching up a paused VM or Docker host)
// reads a live session as started later. Taken for another process, its
// record was removed and it was restored a second time while running.
func TestALinuxProcessIsKnownByItsBootAndTicksNotTheClock(t *testing.T) {
	const pid = 4242
	f := newFakeProc(t, pid)
	const boot = "3f2c9a10-7d4e-4b8a-9c11-0e5d6f7a8b9c"
	f.set(123456, boot, 1_800_000_000)
	id, ok := procIdentity(pid)
	if !ok || id != boot+":123456" {
		t.Fatalf("identity %q %v", id, ok)
	}
	start, _ := procStartTime(pid)
	rec := Active{PID: pid, StartedAt: start, PidStartedAt: start, PidIdent: id}
	if rec.reusedPID() {
		t.Fatal("the writer itself read as another process")
	}

	// The clock stepped 30 s forward: btime moves, the process does not.
	f.set(123456, boot, 1_800_000_030)
	if moved, _ := procStartTime(pid); !moved.After(start.Add(pidKernelTolerance)) {
		t.Fatalf("control: the fake step did not move the start time (%v, %v): the case below proves nothing", start, moved)
	}
	steady := startTimeSteady
	startTimeSteady = true
	if !(Active{PID: pid, PidStartedAt: start}).reusedPID() {
		t.Fatal("control: the start-time compare does not see the step: the case below proves nothing")
	}
	startTimeSteady = steady
	if rec.reusedPID() {
		t.Fatal("a live session read as another process for a clock stepped forward: removed, and restored twice")
	}

	// Another process at its PID: a later start in this boot, or any in another.
	f.set(123456+3000, boot, 1_800_000_030)
	if !rec.reusedPID() {
		t.Fatal("a PID taken again 30 s on read as the same process: its session would never be restored")
	}
	f.set(123456, "0e5d6f7a-0000-4000-8000-000000000000", 1_800_000_030)
	if !rec.reusedPID() {
		t.Fatal("a process of another boot, at the same ticks, read as the same process")
	}

	// A record from before PidIdent keeps the wide tolerance here: its start
	// time moves with the clock.
	f.set(123456, boot, 1_800_000_030)
	if (Active{PID: pid, StartedAt: start, PidStartedAt: start}).reusedPID() {
		t.Fatal("a record without PidIdent lost its tolerance on Linux")
	}
	f.set(123456, boot, 1_800_000_000+int64(2*pidReuseTolerance/time.Second))
	if !(Active{PID: pid, StartedAt: start, PidStartedAt: start}).reusedPID() {
		t.Fatal("a record without PidIdent never reads a process far later as another")
	}
}
