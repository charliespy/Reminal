// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build darwin

package client

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"reminal/internal/proc"
)

// Closed-lid mode, screen half. With sleep disabled, a shut MacBook keeps its
// built-in screen online and lit behind the lid. `reminal-capture liddim`
// watches the lid and turns that screen's brightness to 0 while it is shut,
// putting it back when it opens (native/reminal-capture/main.swift). The
// census owner in vdisplayLoop keeps one running while closed-lid mode is on.

// lidDimStopGrace is how long a stopping dimmer gets to put the brightness back
// before it is killed. Restoring is one framework call; this is generous.
const lidDimStopGrace = 3 * time.Second

// lidDimStatePath is where the dimmer keeps the brightness it took away, so a
// dimmer killed with the lid shut never loses it.
func lidDimStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".reminal", "liddim-brightness")
}

// lidDimLog is where the dimmer's lines go (lid shut and open, what it dimmed
// and restored, with the values and the time). The daemon's stderr is
// ~/.reminal/daemon.log, so it keeps them there. A session's stderr is the
// terminal it runs in, which must never see them: a session that stands in for
// a missing daemon writes them to ~/.reminal/liddim.log instead.
func lidDimLog(isDaemon bool) io.Writer {
	if isDaemon {
		return &lineLogger{prefix: "reminal: liddim: "}
	}
	return &lineLogger{prefix: "reminal: liddim: ", out: &appendFile{path: lidDimLogPath()}}
}

// lidDimPidPath names the process whose dimmer is running: written when one
// starts, removed when it ends. A process that stops standing in for the
// census owner reads it to know whether the owner's dimmer has the screen.
func lidDimPidPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".reminal", "liddim.pid")
}

// dimmingElsewhere reports whether another live process's dimmer is running.
func dimmingElsewhere() bool {
	p := lidDimPidPath()
	if p == "" {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid > 0 && pid != os.Getpid() && proc.Alive(pid)
}

func lidDimLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".reminal", "liddim.log")
}

// appendFile appends to the file at path, opened on the first write. With no
// path, or one that cannot be opened, the lines are dropped.
type appendFile struct {
	path string
	once sync.Once
	f    *os.File
}

func (a *appendFile) Write(p []byte) (int, error) {
	a.once.Do(func() {
		if a.path != "" {
			a.f, _ = os.OpenFile(a.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		}
	})
	if a.f == nil {
		return len(p), nil
	}
	return a.f.Write(p)
}

type lidDimmer struct {
	// log takes the helper's output (lidDimLog).
	log   io.Writer
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{}
	// off: the helper cannot dim on this Mac (no lid, no DisplayServices, or a
	// helper too old to know the subcommand). A Mac does not grow a lid, so
	// this holds for the life of the process.
	off bool
	// heldForOwner: kept one tick after another process took the census, for
	// its dimmer to start (vdisplayLoop).
	heldForOwner bool
}

// ensure starts the dimmer unless it is running or cannot run here.
func (l *lidDimmer) ensure() {
	if l.cmd != nil && stoppedProcess(l.cmd.Process.Pid) {
		// Stopped (SIGSTOP), it would keep the screen dark and never answer the
		// lid. It is ended, the brightness put back, and a new one started.
		l.stop()
	}
	if l.cmd != nil {
		select {
		case <-l.done:
			// It ended on its own. A non-zero exit is the helper saying it
			// cannot do this here; anything else (killed by hand) is restarted.
			if l.cmd.ProcessState != nil && l.cmd.ProcessState.ExitCode() > 0 {
				l.off = true
			}
			l.cmd, l.stdin, l.done = nil, nil, nil
		default:
			return
		}
	}
	if l.off {
		return
	}
	helper, err := captureHelperPath()
	state := lidDimStatePath()
	if err != nil || state == "" {
		return
	}
	cmd := helperCommand(helper, "liddim", state)
	cmd.Stdout = l.log
	stdin, err := cmd.StdinPipe() // lifeline: EOF tells it to restore and exit
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	l.cmd, l.stdin, l.done = cmd, stdin, done
	if p := lidDimPidPath(); p != "" {
		_ = os.WriteFile(p, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
	}
}

// stop ends the dimmer, giving it the time to put the brightness back first,
// and then puts back a brightness a killed dimmer left in the state file.
// Only the census owner calls it: the state file is the owner's, and while a
// live dimmer elsewhere has the screen dark behind a shut lid, the record is
// that dimmer's and putting it back would light the screen under the lid.
func (l *lidDimmer) stop() {
	l.end(false)
	if !l.off {
		restoreLeftover(l.log)
	}
}

// yield ends a dimmer of our own, if there is one, and touches nothing else.
// It is what a process that does not own the census calls: the owner's dimmer
// and its state file are not ours. When the owner's dimmer is running, ours
// hands off — ends without putting the brightness back, so the screen is not
// lit behind a shut lid until the owner's dims it again; the owner's restores
// from the record when the lid opens. With no other dimmer running (an owner
// too old to have one) ours restores as it ends, as before.
func (l *lidDimmer) yield() {
	l.end(l.cmd != nil && dimmingElsewhere())
}

// end ends our dimmer: by EOF, which restores, or after a "handoff" line,
// which does not.
func (l *lidDimmer) end(handoff bool) {
	if l.cmd == nil {
		return
	}
	if handoff {
		_, _ = io.WriteString(l.stdin, "handoff\n")
	}
	_ = l.stdin.Close()
	if stoppedProcess(l.cmd.Process.Pid) {
		_ = l.cmd.Process.Kill() // a stopped helper reads no EOF
	}
	waitOrKill(l.cmd, l.done)
	l.cmd, l.stdin, l.done, l.heldForOwner = nil, nil, nil, false
	// The name of the running dimmer goes with ours, and only ours.
	if p := lidDimPidPath(); p != "" {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == strconv.Itoa(os.Getpid()) {
			_ = os.Remove(p)
		}
	}
}

// stoppedProcess reports whether pid is stopped (SIGSTOP, SIGTSTP): alive,
// but running nothing.
func stoppedProcess(pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && kp.Proc.P_stat == sstop
}

// sstop is SSTOP from <sys/proc.h>: the process is stopped.
const sstop = 4

// restoreLeftover puts back a brightness that a dimmer killed with the lid
// shut left in the state file: a short run whose stdin is /dev/null restores
// and exits at once.
func restoreLeftover(log io.Writer) {
	state := lidDimStatePath()
	if state == "" {
		return
	}
	if _, err := os.Stat(state); err != nil {
		return
	}
	helper, err := captureHelperPath()
	if err != nil {
		return
	}
	cmd := helperCommand(helper, "liddim", state)
	cmd.Stdout = log
	if err := cmd.Start(); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	waitOrKill(cmd, done)
}

func waitOrKill(cmd *exec.Cmd, done <-chan struct{}) {
	if cmd.Process == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(lidDimStopGrace):
		_ = cmd.Process.Kill()
		<-done
	}
}
