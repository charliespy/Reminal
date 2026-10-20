// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !linux

package session

// procIdentity is Linux's (procstart_linux.go). Elsewhere a process's start
// time is kept as it was taken, so no clock step moves it: reusedPID compares
// those.
func procIdentity(pid int) (string, bool) { return "", false }
