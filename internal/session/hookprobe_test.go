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

// helperSleepEnv makes this test binary, started again, only wait (helperSleep).
const helperSleepEnv = "HOOKPROBE_TEST_SLEEP"

// helperSleep is this test binary standing in for a program a probe runs:
// started with helperSleepEnv set, it waits to be killed. Not /bin/sleep:
// macOS keeps an Apple platform binary's environment from every other
// process (kern.procargs2 gives none of it), the same user's too, so a mark
// in one cannot be read from outside — the probe's session record
// (RunProbe) is what says those.
func helperSleep(ctx context.Context, probe bool) *exec.Cmd {
	args := []string{"-test.run=^TestHelperSleep$"}
	var cmd *exec.Cmd
	if probe {
		cmd = ProbeCommand(ctx, os.Args[0], args...)
	} else {
		cmd = exec.CommandContext(ctx, os.Args[0], args...)
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, helperSleepEnv+"=1")
	return cmd
}

// TestHelperSleep is helperSleep's: it waits only when started as one.
func TestHelperSleep(t *testing.T) {
	if os.Getenv(helperSleepEnv) == "" {
		t.Skip("only as a probe's stand-in")
	}
	time.Sleep(time.Minute)
}

// A process started as a probe carries its mark, read from outside it; one
// started plainly does not.
func TestAProbeIsMarkedSeenFromOutside(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("read from outside on Linux and macOS only")
	}
	start := func(cmd *exec.Cmd) int {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		time.Sleep(100 * time.Millisecond) // its exec done
		return cmd.Process.Pid
	}
	probe := start(helperSleep(context.Background(), true))
	plain := start(helperSleep(context.Background(), false))
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

// A probe's record is written, then what probes that died left is let go:
// a record whose leader is gone, and a half-written one whose pid no process
// has — never a half-written one whose process runs (a probe being
// recorded at that moment).
func TestAProbeLetsGoOfWhatDeadProbesLeft(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("sessions read on Linux and macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	dir, err := probesDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const gone = 2147483000 // no process has it
	live := strconv.Itoa(os.Getpid()) + ".tmp"
	for name, body := range map[string]string{strconv.Itoa(gone): "1", strconv.Itoa(gone) + ".tmp": "1", live: "1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RunProbe(ProbeCommand(context.Background(), "/bin/sh", "-c", "true"), false); err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, " ") != live {
		t.Fatalf("left %q, want only %q (a running process's half-written record)", left, live)
	}
}
