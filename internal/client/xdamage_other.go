// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !linux

package client

// winDamage is X11 Damage (xdamage_linux.go): there is none here, and a
// stream captures on its timer.
type winDamage struct{}

func newWinDamage(windowBackend, winInfo) *winDamage { return nil }

func (*winDamage) setRect(winInfo)    {}
func (*winDamage) C() <-chan struct{} { return nil }
func (*winDamage) ok() bool           { return false }
func (*winDamage) close()             {}
