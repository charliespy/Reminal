// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !linux && !darwin && !windows

package pty

// procMark cannot be read here: a hot restart's shell is known by its pid
// alone, as before.
func procMark(pid int) (mark string, ppid int, ok bool) { return "", 0, false }
