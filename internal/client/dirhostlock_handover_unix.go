// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// dirHostLockFDEnv names the fd of the directory-host lock a daemon hands
// its new image as it re-execs in place (execDaemonInPlace).
const dirHostLockFDEnv = "REMINAL_DIRHOST_LOCK_FD"

// dirHostHandover is what a binary's `version --json` says, in "handover",
// when it takes a lock handed to it so (adoptDirHostLock). One that does not
// would keep the fd open and never use it: that process holding the lock
// it then cannot take, the machine served by nobody.
const dirHostHandover = "dirhost-lock-fd"

// dirHostHandoverOK is whether exe takes a handed lock: what its `version
// --json` says (a test replaces it). The cap ends a binary that never
// answers.
var dirHostHandoverOK = func(exe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, exe, "version", "--json").Output()
	if err != nil {
		return false
	}
	var v map[string]any
	if json.Unmarshal(out, &v) != nil {
		return false
	}
	h, _ := v["handover"].(string)
	for _, w := range strings.Fields(h) {
		if w == dirHostHandover {
			return true
		}
	}
	return false
}

// dirHostHandoff is the environment for this daemon's image on exe, and the
// fd of the directory-host lock to hand it (-1: none — none held, or exe
// would not take it).
func dirHostHandoff(exe string) (env []string, fd int) {
	env = withoutDirHostLockFD(os.Environ())
	dirHostMu.Lock()
	f := dirHostHeld
	dirHostMu.Unlock()
	if f == nil || !dirHostHandoverOK(exe) {
		return env, -1
	}
	fd = int(f.Fd())
	return append(env, dirHostLockFDEnv+"="+strconv.Itoa(fd)), fd
}

// adoptDirHostLock takes the directory-host lock this process's image before
// it handed it (dirHostHandoff), as held: tryLockDirHost gives it first. The
// fd must be the lock file still, and the lock its own; anything else is
// left alone. The variable is not handed on.
func adoptDirHostLock() {
	v, ok := os.LookupEnv(dirHostLockFDEnv)
	if !ok {
		return
	}
	_ = os.Unsetenv(dirHostLockFDEnv)
	fd, err := strconv.Atoi(v)
	if err != nil || fd < 3 {
		return
	}
	path, err := lockFilePath(dirHostLockName)
	if err != nil {
		return
	}
	var got, want syscall.Stat_t
	if syscall.Fstat(fd, &got) != nil || syscall.Stat(path, &want) != nil || got.Dev != want.Dev || got.Ino != want.Ino {
		return
	}
	syscall.CloseOnExec(fd)
	f := os.NewFile(uintptr(fd), path)
	// Already this file description's: taking it again only says so.
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		_ = f.Close()
		return
	}
	dirHostMu.Lock()
	dirHostAdopted = f
	dirHostMu.Unlock()
}

// withoutDirHostLockFD is env less dirHostLockFDEnv.
func withoutDirHostLockFD(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, dirHostLockFDEnv+"=") {
			out = append(out, kv)
		}
	}
	return out
}
