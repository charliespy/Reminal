// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"reminal/internal/session"
)

// A restored session's coding agent is resumed only once the session is
// reachable: never in the race with its own registration.
func TestARestoreResumesItsAgentOnlyOnceTheSessionIsReachable(t *testing.T) {
	a := &Agent{}
	planned := make(chan struct{})
	a.restorePlan = func() (string, string) {
		close(planned)
		runtime.Goexit() // the plan is the probe: nothing after it is under test
		return "", ""
	}
	go a.restoreStart()
	select {
	case <-planned:
		t.Fatal("its agent's resume was worked out before the session was reachable")
	case <-time.After(300 * time.Millisecond):
	}
	a.signalRegistered() // no handshake pipe: only the session reachable
	select {
	case <-planned:
	case <-time.After(5 * time.Second):
		t.Fatal("its agent was never resumed once the session was reachable")
	}
	a.signalRegistered() // a second registration changes nothing
}

func TestAConversationTooBigForTheMachineIsNotResumedButSaid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	const conv = "5bc8fbbc-bde5-4099-8164-d8399f767c45"
	dir := filepath.Join(home, ".claude", "projects", "-work")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, conv+".jsonl")
	write := func(size int64) {
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil { // sparse: nothing written
			t.Fatal(err)
		}
		f.Close()
	}
	old := memTotal
	defer func() { memTotal = old }()
	r := session.Restore{Fg: "claude", Conv: conv}
	memTotal = func() int64 { return 1 << 30 }
	write(300 << 20)
	if why := resumeTooBig(r); !strings.Contains(why, "300 MB") || !strings.Contains(why, "claude --resume "+conv) {
		t.Errorf("a 300 MB conversation on 1 GiB: %q", why)
	}
	write(100 << 20)
	if why := resumeTooBig(r); why != "" {
		t.Errorf("a 100 MB conversation on 1 GiB was not resumed: %q", why)
	}
	write(300 << 20)
	memTotal = func() int64 { return 4 << 30 }
	if why := resumeTooBig(r); why != "" {
		t.Errorf("a 300 MB conversation on 4 GiB was not resumed: %q", why)
	}
	memTotal = func() int64 { return 0 }
	if why := resumeTooBig(r); why != "" {
		t.Errorf("a machine whose memory is not known did not resume: %q", why)
	}
	memTotal = func() int64 { return 1 << 30 }
	for _, other := range []session.Restore{{Fg: "codex", Conv: conv}, {Fg: "claude"}, {Fg: "claude", Conv: "*"}, {Fg: "claude", Conv: "00000000-0000-4000-8000-000000000000"}} {
		if why := resumeTooBig(other); why != "" {
			t.Errorf("%+v: %q", other, why)
		}
	}
}

func TestACgroupLimitCountsAsTheMachinesMemory(t *testing.T) {
	for in, want := range map[string]int64{"1073741824\n": 1 << 30, "max\n": 0, "9223372036854771712": 0, "": 0, "-1": 0, "x": 0} {
		if got := parseMemLimit(in); got != want {
			t.Errorf("parseMemLimit(%q) = %d, want %d", in, got, want)
		}
	}
}

// The resume is let go once the session's first try at the relay is over,
// whatever came of it: a relay that is up but refusing (a 5xx, a timeout,
// another agent still registered after a restart) never holds a session's
// agent back — nor does the parent's handshake fire for it.
func TestAFailedFirstTryAtTheRelayStillLetsTheAgentResume(t *testing.T) {
	a := &Agent{}
	a.afterAttempt(errors.New("relay: 503 Service Unavailable"))
	select {
	case <-a.resumeReady():
	default:
		t.Fatal("a refused first try at the relay held the resume back")
	}
	b := &Agent{}
	b.afterAttempt(nil) // a connection that ended without an error: also over
	select {
	case <-b.resumeReady():
	default:
		t.Fatal("a first try that ended held the resume back")
	}
	b.afterAttempt(errors.New("again")) // later tries change nothing
}

func TestAResumeRunsAtALowerPriorityInAPOSIXShell(t *testing.T) {
	old := lookNice
	defer func() { lookNice = old }()
	lookNice = func(string) (string, error) { return "/usr/bin/nice", nil }
	argv := []string{"claude", "--resume", "x"}
	if runtime.GOOS != "windows" {
		if got := lowPriority(argv, "/bin/bash"); !reflect.DeepEqual(got, append([]string{"nice", "-n", "10"}, argv...)) {
			t.Errorf("bash: %q", got)
		}
	}
	for _, sh := range []string{"pwsh", `C:\Windows\System32\cmd.exe`, "/usr/bin/powershell"} {
		if got := lowPriority(argv, sh); !reflect.DeepEqual(got, argv) {
			t.Errorf("%s: %q", sh, got)
		}
	}
	lookNice = func(string) (string, error) { return "", errors.New("no nice") }
	if got := lowPriority(argv, "/bin/bash"); !reflect.DeepEqual(got, argv) {
		t.Errorf("no nice: %q", got)
	}
}
