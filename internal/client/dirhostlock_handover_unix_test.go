// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestDirHostLockHelper is a process of the machine: a daemon that holds the
// directory-host lock and, told "exec", re-execs itself in place, then waits
// to be told "go" before it looks for the lock again; or a session standing
// by, that tries for the lock each time it is told "poll". It says what it
// sees on stdout. Not a test of its own.
func TestDirHostLockHelper(t *testing.T) {
	role := os.Getenv("REMINAL_DIRHOST_ROLE")
	if role == "" {
		t.Skip("run by TestADaemonReexecdInPlaceKeepsTheDirHostLock")
	}
	in := bufio.NewScanner(os.Stdin)
	say := func(s string) { os.Stdout.WriteString(s + "\n") }
	switch role {
	case "daemon":
		if os.Getenv("REMINAL_DIRHOST_EXECED") != "" {
			// The new image: as RunDaemon begins.
			adoptDirHostLock()
			say("EXECED")
			for in.Scan() && in.Text() != "go" {
			}
			_, held := tryLockDirHost()
			say("HELD-AFTER " + strconv.FormatBool(held))
			return
		}
		if _, held := tryLockDirHost(); !held {
			t.Fatal("the daemon could not take the lock")
		}
		say("HELD")
		for in.Scan() && in.Text() != "exec" {
		}
		dirHostHandoverOK = func(string) bool { return true } // this binary has no `version --json`
		os.Setenv("REMINAL_DIRHOST_EXECED", "1")
		t.Fatalf("exec in place: %v", execDaemonInPlace(os.Args[0]))
	case "session":
		say("READY")
		for in.Scan() {
			if in.Text() == "poll" {
				if f, held := tryLockDirHost(); held {
					say("TOOK")
					unlockDirHost(f)
				} else {
					say("BUSY")
				}
			}
		}
	}
}

// helperProc is a TestDirHostLockHelper process in home, as role.
type helperProc struct {
	cmd   *exec.Cmd
	in    io.WriteCloser
	lines *bufio.Scanner
}

func startDirHostHelper(t *testing.T, home, role string) *helperProc {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestDirHostLockHelper$")
	cmd.Env = append(os.Environ(), "REMINAL_TEST_HOME="+home, "REMINAL_DIRHOST_ROLE="+role, "REMINAL_DIRHOST_EXECED=")
	cmd.Stderr = os.Stderr
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return &helperProc{cmd: cmd, in: in, lines: bufio.NewScanner(out)}
}

// said is the next line the helper says that is one of its words (the test
// binary's own output aside).
func (h *helperProc) said(t *testing.T) string {
	t.Helper()
	for h.lines.Scan() {
		l := h.lines.Text()
		for _, w := range []string{"HELD", "EXECED", "READY", "TOOK", "BUSY"} {
			if strings.HasPrefix(l, w) {
				return l
			}
		}
	}
	t.Fatal("the helper ended without saying it")
	return ""
}

func (h *helperProc) tell(s string) { _, _ = h.in.Write([]byte(s + "\n")) }

// A daemon re-exec'd in place onto a new binary (no service manager to
// restart it) keeps the directory-host lock across the exec: a session
// standing by, trying for it while the new image has not yet looked for it,
// never gets it, and the new image serves. Before, the exec let it go, the
// session took it and kept it — the machine served from the session's old
// binary, the daemon standing by behind it for good.
func TestADaemonReexecdInPlaceKeepsTheDirHostLock(t *testing.T) {
	home := isolateHome(t)
	d := startDirHostHelper(t, home, "daemon")
	if got := d.said(t); got != "HELD" {
		t.Fatalf("the daemon said %q", got)
	}
	s := startDirHostHelper(t, home, "session")
	if got := s.said(t); got != "READY" {
		t.Fatalf("the session said %q", got)
	}
	s.tell("poll")
	if got := s.said(t); got != "BUSY" {
		t.Fatalf("while the daemon holds it, the session: %q", got)
	}
	d.tell("exec")
	if got := d.said(t); got != "EXECED" {
		t.Fatalf("the daemon, re-exec'd: %q", got)
	}
	// The gap: re-exec'd, and not yet looked for the lock.
	s.tell("poll")
	gap := s.said(t)
	d.tell("go")
	after := d.said(t)
	if gap != "BUSY" || after != "HELD-AFTER true" {
		t.Fatalf("across the daemon's exec in place: the session, trying in between, %s; the daemon, after, %q", gap, after)
	}
}

// A session stands by for a daemon running here (daemonRunning), never for a
// daemon.pid whose pid is now another program's.
func TestASessionStandsByOnlyForARunningDaemon(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("what a pid is, the system says on Linux")
	}
	home := isolateHome(t)
	pidFile := filepath.Join(home, ".reminal", "daemon.pid")
	if err := os.MkdirAll(filepath.Dir(pidFile), 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) *exec.Cmd {
		c := &exec.Cmd{Path: args[0], Args: args[1:], Env: append(os.Environ(), "REMINAL_TEST_STAND_IN=1")}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
		return c
	}
	daemon := run(os.Args[0], "reminal", "daemon")
	other := run("/bin/sleep", "sleep", "300")
	for _, c := range []struct {
		pid  int
		want bool
		what string
	}{{daemon.Process.Pid, true, "a daemon"}, {other.Process.Pid, false, "another program (a daemon's pid since reused)"}} {
		if err := os.WriteFile(pidFile, []byte(strconv.Itoa(c.pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := daemonRunning(); got != c.want {
			t.Errorf("daemon.pid naming %s: daemonRunning %v, want %v", c.what, got, c.want)
		}
	}
}

// The lock is handed only to a binary whose `version --json` says it takes
// it: one that does not would hold it open, unused, the machine served by
// nobody.
func TestTheDirHostLockIsHandedOnlyToABinaryThatTakesIt(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name, says string
		want       bool
	}{
		{"takes", `{"version":"9.9.9","handover":"dirhost-lock-fd"}`, true},
		{"older", `{"version":"3.17.30"}`, false},
		{"not json", `reminal 1.0`, false},
	} {
		exe := filepath.Join(dir, c.name)
		if err := os.WriteFile(exe, []byte("#!/bin/sh\n[ \"$1 $2\" = \"version --json\" ] && echo '"+c.says+"'\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := dirHostHandoverOK(exe); got != c.want {
			t.Errorf("%s (%s): handed %v, want %v", c.name, c.says, got, c.want)
		}
	}
	if dirHostHandoverOK(filepath.Join(dir, "missing")) {
		t.Error("a binary that is not there: handed")
	}
}

// A handed fd that is not the lock file (replaced since, or a stray
// variable) is not taken, and is close-on-exec from then on: no shell a
// session starts inherits it.
func TestAHandedFDNotTakenIsNotInherited(t *testing.T) {
	home := isolateHome(t)
	other := filepath.Join(home, "not-the-lock")
	fd, err := syscall.Open(other, syscall.O_RDWR|syscall.O_CREAT, 0o600) // no O_CLOEXEC, as handed
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	t.Setenv(dirHostLockFDEnv, strconv.Itoa(fd))
	adoptDirHostLock()
	dirHostMu.Lock()
	adopted := dirHostAdopted
	dirHostMu.Unlock()
	if adopted != nil {
		t.Fatal("took a file that is not the lock")
	}
	flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if e != 0 {
		t.Fatal(e)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Error("left open across exec: a session's shell would inherit it")
	}
}
