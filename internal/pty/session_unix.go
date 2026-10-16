// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package pty

import (
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

type Session struct {
	ptmx *os.File
	cmd  *exec.Cmd
	// shell is the shell's pid when this Session took over a PTY from a
	// hot-restart (cmd is nil then): the shell is still this process's
	// child, the exec kept the pid, and the old image passed it along.
	// 0 when it wasn't passed (restarted by an older binary).
	shell int
}

func Start(shell string, env ...string) (*Session, error) {
	cmd := exec.Command(shell)
	// Run it as a LOGIN shell — the mechanism login(1) and Terminal.app use:
	// argv[0] prefixed with "-". A login shell sources the full profile chain
	// (/etc/zprofile → path_helper, ~/.zprofile → `brew shellenv`, ~/.zshrc, …),
	// so the session's PATH and environment match a freshly-opened terminal rather
	// than whatever the spawning process happened to inherit. This is essential for
	// sessions started by the background host, which runs under launchd/systemd
	// with a bare PATH: a non-login shell there would be missing Homebrew, claude,
	// and everything else set up in the login profile — the session wouldn't behave
	// like a real terminal at all.
	cmd.Args[0] = "-" + filepath.Base(shell)
	cmd.Env = shellEnv(env)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return &Session{ptmx: ptmx, cmd: cmd}, nil
}

// Attach wraps an already-open PTY master fd as a Session. Used by the
// hot-restart path (syscall.Exec into a new binary) to take over the
// existing shell without re-spawning it. The shell process continues
// running unaware of the swap because its parent PID is preserved
// across Exec and its controlling terminal — the slave side of this
// PTY — is untouched. cmd is left nil; the new process didn't spawn
// the shell so there's no Cmd to Wait on. EOF on the master fd
// signals shell exit instead. shellPid is the shell's pid as the old image
// knew it, or 0 if it didn't say.
func Attach(ptmx *os.File, shellPid int) *Session {
	return &Session{ptmx: ptmx, cmd: nil, shell: shellPid}
}

func (s *Session) Read(p []byte) (int, error) {
	return s.ptmx.Read(p)
}

func (s *Session) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

func (s *Session) Resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Getsize reports the PTY master's current window size. The hot-restart path
// inherits an already-sized PTY, so this recovers the dimensions the shell is
// actually rendering at before any viewer has reported a size.
func (s *Session) Getsize() (cols, rows uint16, err error) {
	ws, err := pty.GetsizeFull(s.ptmx)
	if err != nil {
		return 0, 0, err
	}
	return ws.Cols, ws.Rows, nil
}

func (s *Session) Wait() error {
	return s.cmd.Wait()
}

func (s *Session) Close() error {
	return s.ptmx.Close()
}

// Fd returns the underlying file descriptor of the PTY master. Used by
// the hot-restart path to pass the open PTY to the new binary across a
// syscall.Exec boundary.
func (s *Session) Fd() uintptr {
	return s.ptmx.Fd()
}

// Pid returns a PID representing what's running in the session, used to read
// the live working directory for `reminal list`.
//
// When we spawned the shell we have its PID directly, and after a hot-restart
// the old image passes it along (ShellPid). Only when it didn't (restarted by
// an older binary) do we fall back to the terminal's foreground process group
// (TIOCGPGRP on the master), which is the shell only while it is at its
// prompt. 0 if neither is available.
func (s *Session) Pid() int {
	if pid := s.ShellPid(); pid > 0 {
		return pid
	}
	if s.ptmx != nil {
		if pgrp, err := unix.IoctlGetInt(int(s.ptmx.Fd()), unix.TIOCGPGRP); err == nil && pgrp > 0 {
			return pgrp
		}
	}
	return 0
}

// ShellPid is the session's shell's pid when it is known for certain: we
// spawned it, or a hot-restart passed it along and it is still running. 0
// otherwise. Unlike Pid it never guesses, so it is what a restart passes on.
func (s *Session) ShellPid() int {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	if s.shell > 0 && unix.Kill(s.shell, 0) == nil {
		return s.shell
	}
	return 0
}

// ForegroundPgrp returns the PTY's current foreground process group (TIOCGPGRP
// on the master), or 0 if unavailable. Unlike Pid, it ALWAYS reports what is
// actually in the foreground — the shell itself when idle, or a launched
// program's group when one is running. That lets a caller tell "an agent is
// running" from "sitting at the shell prompt" even for inline TUIs that never
// take the alt screen (e.g. Claude Code): compare it against the shell's pid.
func (s *Session) ForegroundPgrp() int {
	if s.ptmx == nil {
		return 0
	}
	if pgrp, err := unix.IoctlGetInt(int(s.ptmx.Fd()), unix.TIOCGPGRP); err == nil && pgrp > 0 {
		return pgrp
	}
	return 0
}

func (s *Session) CopyFrom(r io.Reader, done chan<- struct{}) {
	defer close(done)
	_, _ = io.Copy(s.ptmx, r)
}

func (s *Session) CopyTo(w io.Writer, done chan<- struct{}) {
	defer close(done)
	_, _ = io.Copy(w, s.ptmx)
}

func HandleSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			// SIGWINCH is handled by the relay resize messages from the viewer.
		}
	}()
}

// EndedBySignal waits (briefly) for the shell and reports whether a signal
// ended it — a machine shutting down, or someone killing it — rather than
// the shell exiting on its own. False for an adopted PTY, whose shell this
// process never started and cannot wait for.
func (s *Session) EndedBySignal() bool {
	if s.cmd == nil || s.cmd.Process == nil {
		return false
	}
	done := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		return false
	}
	return s.cmd.ProcessState != nil && s.cmd.ProcessState.ExitCode() == -1
}

// WriteCancel is Write that stop cuts short: once stop is closed, a write
// held because nothing reads the PTY's input — its queue full — no longer
// holds its caller, who is told it was stopped (errWriteStopped). A PTY
// master is a blocking descriptor (its Fd is taken), which no deadline
// reaches, so a write it holds is let go on: it ends when the PTY is read
// or closed — at once when the session ends with it.
func (s *Session) WriteCancel(p []byte, stop <-chan struct{}) (int, error) {
	return writeCancel(s.ptmx, p, stop)
}
