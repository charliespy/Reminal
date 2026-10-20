// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build linux || darwin

package procgroup

import (
	"context"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

// BoundSession's child leads a session of its own.
func TestBoundSessionLeadsItsOwnSession(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sleep", "5")
	BoundSession(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	sid, err := unix.Getsid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if sid != cmd.Process.Pid {
		t.Fatalf("its session is %d, want its own (%d)", sid, cmd.Process.Pid)
	}
}
