// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package pty

// ShellCgroupEnv names a cgroup v2 directory (an absolute path) the session's
// shell is started in, on Linux, so everything the shell runs is born under
// that cgroup's limits. Only the shell goes there, not reminal itself. Unset or
// empty: the shell starts as it always has. Elsewhere it is ignored. The shell
// never fails to start over it: a directory that is not a cgroup v2 one, or a
// kernel that cannot start a process into one, gets the shell started the
// usual way, and ShellCgroup says why.
const ShellCgroupEnv = "REMINAL_SHELL_CGROUP"

// ShellCgroup is how a ShellCgroupEnv start went: the directory asked for,
// whether the shell was started in it, and why not.
type ShellCgroup struct {
	Dir   string `json:"dir"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}
