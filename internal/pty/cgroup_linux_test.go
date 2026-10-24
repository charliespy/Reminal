// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package pty

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// startSh starts /bin/sh as a session shell with ShellCgroupEnv set to dir.
func startSh(t *testing.T, dir string) *Session {
	t.Helper()
	t.Setenv(ShellCgroupEnv, dir)
	s, err := Start("/bin/sh")
	if err != nil {
		t.Fatalf("the shell did not start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// A directory that is not a cgroup v2 one, or not an absolute path, never
// keeps the shell from starting: it starts the usual way, and says why.
func TestAShellCgroupThatCannotBeUsedStillStartsTheShell(t *testing.T) {
	for _, c := range []struct{ dir, why string }{
		{t.TempDir(), "not a cgroup v2 directory"},
		{"relative/dir", "not an absolute path"},
		{filepath.Join(t.TempDir(), "gone"), "no such file"},
	} {
		s := startSh(t, c.dir)
		cg := s.ShellCgroup()
		if cg == nil || cg.Dir != c.dir || cg.OK || !strings.Contains(cg.Error, c.why) {
			t.Errorf("%s: got %+v, want not ok because %q", c.dir, cg, c.why)
		}
		if s.cmd == nil || s.cmd.Process == nil {
			t.Errorf("%s: no shell", c.dir)
		}
	}
}

// Unset: the shell starts as it always has, and nothing is said.
func TestNoShellCgroupIsTodaysStart(t *testing.T) {
	s := startSh(t, "")
	if cg := s.ShellCgroup(); cg != nil {
		t.Errorf("got %+v with %s unset", cg, ShellCgroupEnv)
	}
}

// The setting is reminal's; the shell's environment does not carry it.
func TestTheShellDoesNotInheritShellCgroup(t *testing.T) {
	t.Setenv(ShellCgroupEnv, "/sys/fs/cgroup/x")
	for _, kv := range shellEnv(nil) {
		if strings.HasPrefix(kv, ShellCgroupEnv+"=") {
			t.Errorf("the shell's environment has %s", kv)
		}
	}
}

// With a cgroup v2 directory it may write to, the shell is born in it: its
// /proc/<pid>/cgroup names that directory. Needs a writable cgroup2 mount
// (REMINAL_CGROUP_TEST=1: scripts/shell-cgroup-test runs it in a container
// with a private cgroup namespace).
func TestTheShellIsStartedInTheShellCgroup(t *testing.T) {
	if os.Getenv("REMINAL_CGROUP_TEST") != "1" {
		t.Skip("set REMINAL_CGROUP_TEST=1 with a writable cgroup v2 mount at /sys/fs/cgroup")
	}
	dir := filepath.Join("/sys/fs/cgroup", "reminal-shell-test")
	if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	s := startSh(t, dir)
	if cg := s.ShellCgroup(); cg == nil || !cg.OK || cg.Error != "" {
		t.Fatalf("got %+v, want ok", cg)
	}
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(s.cmd.Process.Pid) + "/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != "0::/reminal-shell-test" {
		t.Errorf("the shell's cgroup is %q, want 0::/reminal-shell-test", got)
	}
	// Only the shell: this process stays where it was.
	self, _ := os.ReadFile("/proc/self/cgroup")
	if strings.Contains(string(self), "reminal-shell-test") {
		t.Errorf("reminal itself moved into the cgroup: %q", self)
	}
	_ = s.Close()
	for i := 0; i < 50 && os.Remove(dir) != nil; i++ {
		time.Sleep(100 * time.Millisecond)
	}
}
