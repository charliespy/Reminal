// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProbeEnvLeavesOutEveryReminalVariable(t *testing.T) {
	got := ProbeEnv([]string{"HOME=/h", "REMINAL_SESSION=AAAA2345", "PATH=/bin", "REMINAL_RELAY=ws://x", "XREMINAL_KEEP=1"})
	want := []string{"HOME=/h", "PATH=/bin", "XREMINAL_KEEP=1", HookProbeEnv + "=1"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ProbeEnv = %q, want %q", got, want)
	}
}

// A process started as a probe carries its mark, read from outside it; one
// started plainly does not.
func TestAProbeIsMarkedSeenFromOutside(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("read from outside on Linux and macOS only")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep")
	}
	start := func(cmd *exec.Cmd) int {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		time.Sleep(100 * time.Millisecond) // its exec done
		return cmd.Process.Pid
	}
	probe := start(ProbeCommand(context.Background(), sleep, "30"))
	plain := start(exec.Command(sleep, "30"))
	if !probeMarked(probe) {
		t.Fatal("a probe not seen as one")
	}
	if probeMarked(plain) {
		t.Fatal("a plain process seen as a probe")
	}
	if probeMarked(os.Getpid()) {
		t.Fatal("this test seen as a probe")
	}
}

// Enclosing names no session from inside a probe.
func TestEnclosingNamesNoSessionInAProbe(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	if err := WriteActive(Active{ID: "ANCESTOR", PID: os.Getppid()}); err != nil {
		t.Fatal(err)
	}
	if got := Enclosing(); got != "ANCESTOR" {
		t.Fatalf("Enclosing = %q, want ANCESTOR", got)
	}
	t.Setenv(HookProbeEnv, "1")
	if got := Enclosing(); got != "" {
		t.Fatalf("Enclosing inside a probe = %q, want none", got)
	}
}
