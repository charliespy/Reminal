// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !linux && !darwin

package session

// processEnv is not read here: a probe's own mark, in its environment and
// what it starts, is what says it (InProbe).
func processEnv(pid int) []string { return nil }

// No sessions read here: a probe's own mark says it (RunProbe records none).
func processSession(pid int) int { return 0 }

func probeIdent(pid int) (string, bool) { return "", false }
