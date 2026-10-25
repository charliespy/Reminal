// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

// The daemon never re-execs in place here (sig_windows.go): nothing to hand.
func adoptDirHostLock() {}
