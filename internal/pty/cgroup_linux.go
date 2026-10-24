// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux

package pty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// startShell starts the command newCmd builds on a new PTY. With
// ShellCgroupEnv set, the shell is started straight into that cgroup v2
// directory (clone3 with CLONE_INTO_CGROUP), and if that cannot be done, the
// usual way. cg is nil when ShellCgroupEnv is unset.
func startShell(newCmd func() *exec.Cmd) (ptmx *os.File, cmd *exec.Cmd, cg *ShellCgroup, err error) {
	dir := os.Getenv(ShellCgroupEnv)
	if dir == "" {
		cmd = newCmd()
		ptmx, err = pty.Start(cmd)
		return ptmx, cmd, nil, err
	}
	cg = &ShellCgroup{Dir: dir}
	fd, oerr := openCgroupDir(dir)
	if oerr == nil {
		cmd = newCmd()
		ptmx, err = pty.StartWithAttrs(cmd, nil, cgroupAttrs(cmd.SysProcAttr, fd))
		_ = unix.Close(fd)
		if err == nil {
			cg.OK = true
			return ptmx, cmd, cg, nil
		}
		// An old kernel (ENOSYS, EINVAL), a cgroup that takes no process
		// (EBUSY), one this user may not write (EACCES, EPERM): the shell
		// starts all the same, the usual way.
		oerr = err
	}
	cg.Error = oerr.Error()
	// A Cmd that failed to start cannot be started again: a fresh one.
	cmd = newCmd()
	ptmx, err = pty.Start(cmd)
	return ptmx, cmd, cg, err
}

// openCgroupDir opens dir for CLONE_INTO_CGROUP: an absolute path to a
// directory on a cgroup v2 file system.
func openCgroupDir(dir string) (int, error) {
	if !filepath.IsAbs(dir) {
		return -1, errors.New("not an absolute path")
	}
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return -1, err
	}
	if st.Type != unix.CGROUP2_SUPER_MAGIC {
		return -1, fmt.Errorf("not a cgroup v2 directory")
	}
	return unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
}

// cgroupAttrs is the command's own process attributes (a copy: the command
// may be started again without them), with what pty.Start sets (a session of
// its own, the PTY as its controlling terminal: its stdin, fd 0) and the
// cgroup to start in. Whatever else the command set survives, as it does on
// the pty.Start path.
func cgroupAttrs(base *syscall.SysProcAttr, fd int) *syscall.SysProcAttr {
	a := &syscall.SysProcAttr{}
	if base != nil {
		c := *base
		a = &c
	}
	a.Setsid, a.Setctty, a.Ctty = true, true, 0
	a.UseCgroupFD, a.CgroupFD = true, fd
	return a
}
