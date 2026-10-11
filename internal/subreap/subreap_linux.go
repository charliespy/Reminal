// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package subreap

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
)

// Run starts the command and collects its whole tree; it returns the exit
// code to leave with: the command's own, or 128+n if signal n ended it.
//
// It blocks in wait4(-1) — every process it collects is the command or an
// orphan of its tree: this process starts nothing else, so there is no other
// waiter to take a status from. No timer, no looking: each end is collected
// when the kernel says it happened.
func Run(args []string) (int, error) {
	argv, err := command(args)
	if err != nil {
		return 2, err
	}
	if err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0); err != nil {
		return 1, err
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return 127, err
	}
	// Signals a person or a supervisor sends this process are the command's.
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP, syscall.SIGQUIT)
	defer signal.Stop(sigs)
	p, err := os.StartProcess(path, argv, &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}, Env: os.Environ()})
	if err != nil {
		return 126, err
	}
	main := p.Pid
	var mainDone atomic.Bool
	go func() {
		for s := range sigs {
			if !mainDone.Load() {
				_ = p.Signal(s)
			}
		}
	}()
	code := 0
	for {
		var ws unix.WaitStatus
		pid, err := unix.Wait4(-1, &ws, 0, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if errors.Is(err, unix.ECHILD) {
			return code, nil // the last of the tree has ended
		}
		if err != nil {
			return 1, err
		}
		if pid == main {
			mainDone.Store(true)
			switch {
			case ws.Exited():
				code = ws.ExitStatus()
			case ws.Signaled():
				code = 128 + int(ws.Signal())
			}
		}
	}
}
