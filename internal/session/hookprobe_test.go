// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

// A probe's session is recorded while it runs, and only then.
func TestARunningProbesSessionIsRecorded(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("sessions read on Linux and macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	dir, err := probesDir()
	if err != nil {
		t.Fatal(err)
	}
	// Itself the leader of its own session: it says its pid, and what is
	// recorded once it has had a moment to be.
	out, err := RunProbe(ProbeCommand(context.Background(), "/bin/sh", "-c", `echo $$; sleep 0.3; ls "`+dir+`"`), false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(out))
	if len(lines) < 2 || lines[0] != lines[1] {
		t.Fatalf("its session not recorded while it ran: %q", out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("its record left behind: %v", entries)
	}
}

// A record whose leader is another process now (its pid used again) refuses
// nothing; one that is its leader's refuses a walk through its session.
func TestEnclosingRefusesOnlyALiveProbesSession(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("sessions read on Linux and macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	if err := WriteActive(Active{ID: "ANCESTOR", PID: os.Getppid()}); err != nil {
		t.Fatal(err)
	}
	sid := processSession(os.Getpid())
	ident, ok := probeIdent(sid)
	if sid <= 0 || !ok {
		t.Skip("this test's session cannot be read")
	}
	dir, _ := probesDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := filepath.Join(dir, strconv.Itoa(sid))
	if err := os.WriteFile(rec, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Enclosing(); got != "ANCESTOR" {
		t.Fatalf("a record for a pid used again refused the walk: %q", got)
	}
	if err := os.WriteFile(rec, []byte(ident), 0o600); err != nil {
		t.Fatal(err)
	}
	// This test's own session recorded as a probe's: the walk starts in it.
	if got := Enclosing(); got != "" {
		t.Fatalf("a walk in a running probe's session named %q", got)
	}
}
