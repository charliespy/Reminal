// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"sync"
)

// The directory-host lock ensures exactly ONE agent on this machine serves the
// owner directory channel at a time. Without it every session's directory host
// races for the same relay channel — and because the production relay
// *supersedes* an agent that reconnects with a matching credential (while the
// local relay *rejects* it), sibling sessions would ping-pong the channel on
// every reclaim, so the machine flaps online/offline and in-flight owner
// handshakes/spawns get dropped.
const dirHostLockName = "dirhost.lock"

// tryLockDirHost attempts to become this machine's sole directory host WITHOUT
// blocking. On success it returns the held lock file — keep it open for the
// lock's lifetime and release it with unlockDirHost.
// A lock that could not be attempted is treated the same as one held by a
// sibling: don't serve the directory channel, and try again on the next poll.
//
// A daemon that was handed the lock by its own image before it — the same
// process, re-exec'd in place onto a new binary (execDaemonInPlace) — is
// given that one, already held (adoptDirHostLock).
func tryLockDirHost() (*os.File, bool) {
	dirHostMu.Lock()
	defer dirHostMu.Unlock()
	if f := dirHostAdopted; f != nil {
		dirHostAdopted, dirHostHeld = nil, f
		return f, true
	}
	f, held, _ := tryLockFile(dirHostLockName)
	if held {
		dirHostHeld = f
	}
	return f, held
}

// unlockDirHost releases the directory-host lock.
func unlockDirHost(f *os.File) {
	dirHostMu.Lock()
	if dirHostHeld == f {
		dirHostHeld = nil
	}
	dirHostMu.Unlock()
	unlockFile(f)
}

// The lock this process holds, and the one its image before it handed it.
// A daemon re-exec'd in place keeps the lock across the exec rather than let
// it go: in between, a session standing by would take it and keep it, the
// machine served from that session's old binary for as long as it lives,
// and the daemon, back on the new one, standing by behind it.
var (
	dirHostMu      sync.Mutex
	dirHostHeld    *os.File
	dirHostAdopted *os.File
)
