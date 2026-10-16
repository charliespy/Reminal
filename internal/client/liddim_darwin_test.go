// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build darwin

package client

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeLidHelper stands in for reminal-capture: it records each run in calls,
// prints a line as the real dimmer does, and holds until its stdin closes (a
// restoring run reads /dev/null and ends at once). It returns the calls path.
func fakeLidHelper(t *testing.T, home string) string {
	t.Helper()
	calls := filepath.Join(home, "calls")
	script := filepath.Join(home, "fake-helper")
	body := "#!/bin/sh\necho \"$*\" >> " + calls + "\necho \"liddim up\"\ncat >/dev/null\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REMINAL_CAPTURE_HELPER", script)
	return calls
}

func readCalls(t *testing.T, calls string) string {
	t.Helper()
	b, err := os.ReadFile(calls)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// lidState writes the record a live dimmer keeps while the screen is dark
// behind a shut lid.
func lidState(t *testing.T) string {
	t.Helper()
	p := lidDimStatePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("0.75\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The flap: with the daemon's dimmer keeping the screen dark behind a shut
// lid, every session deferring to it ran a restoring helper each tick, which
// lit the screen and printed its lines into the session's terminal. A session
// that does not hold the census must never run the helper, nor touch the
// state file — on any tick, or when it exits.
func TestDeferringSessionLeavesTheDimmerAlone(t *testing.T) {
	home := isolateHome(t)
	calls := fakeLidHelper(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".reminal"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".reminal", "settings.json"), []byte(`{"closed_lid":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state := lidState(t)

	// The census owner: a live process that is not us, freshly claimed.
	owner := exec.Command("sleep", "60")
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
	if err := os.WriteFile(vdisplayCensusLeasePath(), []byte(strconv.Itoa(owner.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	defer func(d time.Duration) { vdisplayPoll = d }(vdisplayPoll)
	vdisplayPoll = 10 * time.Millisecond
	stop, done := make(chan struct{}), make(chan struct{})
	go func() { vdisplayLoop(stop, false); close(done) }()
	time.Sleep(300 * time.Millisecond) // many ticks
	close(stop)
	<-done

	if c := readCalls(t, calls); c != "" {
		t.Errorf("a deferring session ran the dimmer helper:\n%s", c)
	}
	if _, err := os.Stat(state); err != nil {
		t.Errorf("a deferring session consumed the owner's state file: %v", err)
	}
	if b, _ := os.ReadFile(vdisplayCensusLeasePath()); strings.TrimSpace(string(b)) != strconv.Itoa(owner.Process.Pid) {
		t.Errorf("a deferring session displaced the owner's lease: %q", b)
	}
}

// The census owner still puts back what a killed dimmer left behind: stop with
// no dimmer of ours and a record on disk runs one restoring helper.
func TestOwnerStopRestoresLeftover(t *testing.T) {
	home := isolateHome(t)
	calls := fakeLidHelper(t, home)
	lidState(t)
	dim := lidDimmer{log: io.Discard}
	dim.yield()
	if c := readCalls(t, calls); c != "" {
		t.Fatalf("yield with no dimmer of ours ran the helper:\n%s", c)
	}
	dim.stop()
	if c := readCalls(t, calls); strings.Count(c, "\n") != 1 || !strings.HasPrefix(c, "liddim ") {
		t.Errorf("stop did not run one restoring helper: %q", c)
	}
}

// A session's stderr is the terminal it runs in. The dimmer's lines from a
// session go to ~/.reminal/liddim.log and never to stderr; the daemon's go to
// its stderr, which is daemon.log.
func TestSessionDimmerLinesStayOutOfTheTerminal(t *testing.T) {
	home := isolateHome(t)
	fakeLidHelper(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".reminal"), 0o700); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = saved }()

	dim := lidDimmer{log: lidDimLog(false)}
	dim.ensure()
	if dim.cmd == nil {
		t.Fatal("the dimmer did not start")
	}
	dim.stop()

	os.Stderr = saved
	_ = w.Close()
	leaked, _ := io.ReadAll(r)
	if strings.Contains(string(leaked), "liddim") {
		t.Errorf("dimmer lines reached a session's stderr: %q", leaked)
	}
	b, _ := os.ReadFile(lidDimLogPath())
	if !strings.Contains(string(b), "reminal: liddim: liddim up") {
		t.Errorf("liddim.log = %q, want the dimmer's lines", b)
	}

	// The daemon's lines go to its stderr.
	r, w, err = os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	_, _ = lidDimLog(true).Write([]byte("lid shut\n"))
	os.Stderr = saved
	_ = w.Close()
	if got, _ := io.ReadAll(r); string(got) != "reminal: liddim: lid shut\n" {
		t.Errorf("daemon stderr = %q", got)
	}
}

// stdinHelper stands in for reminal-capture and keeps what each liddim run
// read on its stdin, in <home>/stdin.<pid> once it ends, so a test can tell
// a handoff from an EOF.
func stdinHelper(t *testing.T, home string) {
	t.Helper()
	script := filepath.Join(home, "fake-helper")
	// Only the dimmer's runs, and each kept once it has ended (renamed in).
	in, kept := filepath.Join(home, "in-"), filepath.Join(home, "stdin.")
	body := "#!/bin/sh\n[ \"$1\" = liddim ] || exec cat >/dev/null\ncat > " + in + "$$ && mv " + in + "$$ " + kept + "$$\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REMINAL_CAPTURE_HELPER", script)
}

func helperStdins(t *testing.T, home string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(home, "stdin.*"))
	var out []string
	for _, f := range m {
		b, _ := os.ReadFile(f)
		out = append(out, string(b))
	}
	return out
}

func liveOther(t *testing.T) int {
	t.Helper()
	c := exec.Command("sleep", "60")
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	return c.Process.Pid
}

// A session that stood in for the census owner and now yields: with the
// owner's dimmer running, ours hands off (no restore as it ends, which would
// light the screen behind a shut lid); with none (an owner too old to have
// one), ours ends by EOF and restores, as before. Its name in liddim.pid is
// written when it starts and goes with it, never another's.
func TestYieldHandsOffOnlyToARunningDimmer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		otherDims   bool
		wantHandoff bool
	}{
		{"the owner's dimmer runs: handoff", true, true},
		{"control: no other dimmer: EOF, restores", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateHome(t)
			stdinHelper(t, home)
			if err := os.MkdirAll(filepath.Join(home, ".reminal"), 0o700); err != nil {
				t.Fatal(err)
			}
			dim := lidDimmer{log: io.Discard}
			dim.ensure()
			if dim.cmd == nil {
				t.Fatal("the dimmer did not start")
			}
			if b, _ := os.ReadFile(lidDimPidPath()); strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
				t.Fatalf("liddim.pid = %q, want our pid", b)
			}
			other := 0
			if tc.otherDims {
				other = liveOther(t)
				if err := os.WriteFile(lidDimPidPath(), []byte(strconv.Itoa(other)+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			dim.yield()
			in := helperStdins(t, home)
			if len(in) != 1 {
				t.Fatalf("helper runs: %q", in)
			}
			if got := in[0] == "handoff\n"; got != tc.wantHandoff {
				t.Errorf("helper read %q, want handoff=%v", in[0], tc.wantHandoff)
			}
			b, err := os.ReadFile(lidDimPidPath())
			if tc.otherDims && strings.TrimSpace(string(b)) != strconv.Itoa(other) {
				t.Errorf("the owner's liddim.pid was touched: %q %v", b, err)
			}
			if !tc.otherDims && err == nil {
				t.Errorf("our liddim.pid outlived our dimmer: %q", b)
			}
		})
	}
}

// The loop end to end: a session that owns the census and dims sees the
// owner come back. With the owner's dimmer running, ours hands off; with none
// ever (an owner too old to have one), ours ends by EOF and restores.
func TestLoopHandsOffWhenTheOwnerReturns(t *testing.T) {
	for _, tc := range []struct {
		name      string
		otherDims bool
		want      string
	}{
		{"the owner's dimmer runs: handoff", true, "handoff\n"},
		{"control: no owner dimmer: EOF", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := isolateHome(t)
			stdinHelper(t, home)
			if err := os.MkdirAll(filepath.Join(home, ".reminal"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".reminal", "settings.json"), []byte(`{"closed_lid":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			defer func(d time.Duration) { vdisplayPoll = d }(vdisplayPoll)
			vdisplayPoll = 20 * time.Millisecond
			stop, done := make(chan struct{}), make(chan struct{})
			go func() { vdisplayLoop(stop, false); close(done) }()
			defer func() { close(stop); <-done }()
			until := func(ok func() bool) bool {
				for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
					if ok() {
						return true
					}
				}
				return false
			}
			if !until(func() bool {
				b, _ := os.ReadFile(lidDimPidPath())
				return strings.TrimSpace(string(b)) == strconv.Itoa(os.Getpid())
			}) {
				t.Fatal("our dimmer never started")
			}
			owner := liveOther(t)
			if tc.otherDims {
				if err := os.WriteFile(lidDimPidPath(), []byte(strconv.Itoa(owner)+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(vdisplayCensusLeasePath(), []byte(strconv.Itoa(owner)+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if !until(func() bool { return len(helperStdins(t, home)) == 1 }) {
				t.Fatal("our dimmer never ended")
			}
			if in := helperStdins(t, home); in[0] != tc.want {
				t.Errorf("helper read %q, want %q", in[0], tc.want)
			}
		})
	}
}
