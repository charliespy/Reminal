// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyAgentSignals registers the signals a long-running agent/tunnel reacts
// to: SIGINT/SIGTERM to shut down, SIGUSR1 to pause broadcasting (`reminal
// stop`). Windows has no SIGUSR1 — pause arrives over the control socket there
// (see sig_windows.go / control.go "pause").
func notifyAgentSignals(ch chan os.Signal) {
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
}

// isPauseSignal reports whether sig is the pause-broadcast request.
func isPauseSignal(sig os.Signal) bool { return sig == syscall.SIGUSR1 }

// execTunnelBinary replaces the current process image with exe (a port
// forward's hot-swap, see Tunnel.execRestart). Never returns on success.
func execTunnelBinary(exe string, args, env []string) error {
	return syscall.Exec(exe, args, env)
}

// RestartPortForward asks a running port forward (by pid) to hot-swap onto the
// binary now on disk, keeping its session id and PIN. The forward treats
// SIGUSR1 as that request (the shell agent uses the same signal for "pause").
func RestartPortForward(pid int) error {
	return syscall.Kill(pid, syscall.SIGUSR1)
}

// watchResize delivers a tick whenever the host terminal's size may have
// changed (SIGWINCH here; a size poller on Windows). The returned stop func
// unregisters the watcher.
func watchResize() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch, func() { signal.Stop(ch) }
}

// respawnDaemonAfterUpgrade is a no-op where a service manager (launchd
// KeepAlive / systemd Restart=always) restarts the exited daemon itself.
// Where none does, the daemon starts itself again in place first
// (execDaemonInPlace, watchBinaryAndExit).
func respawnDaemonAfterUpgrade(exe string) {}

// execDaemonInPlace re-execs the daemon onto exe, the binary now on disk, with
// its own arguments and environment: the same process, and so the same pid in
// daemon.pid. Never returns on success.
//
// The directory-host lock goes with it, held (dirHostHandoff), when exe
// takes it: let go by the exec, a session standing by would take it before
// the new image could, and keep it. Its fd is open across the exec only
// with forks held off, so no child started meanwhile has it.
func execDaemonInPlace(exe string) error {
	env, fd := dirHostHandoff(exe)
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()
	if fd >= 0 {
		if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, 0); e != 0 {
			fd = -1
			env = withoutDirHostLockFD(env)
		}
	}
	err := syscall.Exec(exe, os.Args, env)
	if fd >= 0 {
		syscall.CloseOnExec(fd) // not exec'd: the lock this process's again
	}
	return err
}
