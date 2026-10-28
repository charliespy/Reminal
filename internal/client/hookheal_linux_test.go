// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// standIn writes a copy of this test binary to file and runs it from run (the
// file, or a link to it), with argv0 as its command line's first word ("" for
// run): a process that only sleeps (TestMain's REMINAL_TEST_STAND_IN). Not a
// copy of the system's sleep: on Ubuntu 26.04 that is one multi-call binary
// which, named claude or node, refuses to run at all.
func standIn(t *testing.T, file, run, argv0 string) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, raw, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(run)
	if argv0 != "" {
		cmd.Args[0] = argv0
	}
	cmd.Env = append(os.Environ(), "REMINAL_TEST_STAND_IN=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	time.Sleep(50 * time.Millisecond) // its exec done
	return cmd
}

// The session asks the agent it runs, by the binary its process was started
// from: a native build named for the agent is that binary itself.
func TestTheAgentsOwnBinaryIsWhatIsAsked(t *testing.T) {
	claude := filepath.Join(t.TempDir(), "claude")
	cmd := standIn(t, claude, claude, "")
	if got := agentCommandOf(cmd.Process.Pid, "claude"); len(got) != 1 || got[0] != claude {
		t.Fatalf("asked %q, want %q", got, claude)
	}
	if got := agentCommandOf(os.Getpid(), "claude"); got != nil {
		t.Fatalf("another program read as claude: %q", got)
	}
}

// A native install: ~/.local/bin/claude a link to the versioned binary it
// runs (~/.local/share/claude/versions/<v>). Its process's exe is that file,
// named for its version: started by the agent's name, it is the agent's
// binary all the same (review).
func TestANativeInstallsVersionedBinaryIsAsked(t *testing.T) {
	home := t.TempDir()
	versioned := filepath.Join(home, ".local", "share", "claude", "versions", "2.1.77")
	link := filepath.Join(home, ".local", "bin", "claude")
	for _, d := range []string{filepath.Dir(versioned), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(versioned, link); err != nil {
		t.Fatal(err)
	}
	cmd := standIn(t, versioned, link, "")
	if got := agentCommandOf(cmd.Process.Pid, "claude"); len(got) != 1 || got[0] != versioned {
		t.Fatalf("asked %q, want %q", got, versioned)
	}
}

// A binary is asked its version once while it is the same file: written
// anew (an upgrade, a downgrade), it is another key, asked again.
func TestAnAgentBinaryIsKeptByItsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "claude")
	if err := os.WriteFile(p, []byte("a"), 0o755); err != nil {
		t.Fatal(err)
	}
	k1 := agentBinaryKey(p)
	if k1 == "" || agentBinaryKey(p) != k1 {
		t.Fatalf("not one key for one file: %q", k1)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	if agentBinaryKey(p) == k1 {
		t.Fatal("a binary written anew kept its key")
	}
	if agentBinaryKey(filepath.Join(t.TempDir(), "none")) != "" {
		t.Fatal("a file not there has a key")
	}
}

// An npm install's node names its process "claude" (process.title): its
// interpreter is never asked as if it were claude (review).
func TestAnInterpreterNamedForTheAgentIsNotAsked(t *testing.T) {
	node := filepath.Join(t.TempDir(), "node")
	cmd := standIn(t, node, node, "claude")
	if got := agentCommandOf(cmd.Process.Pid, "claude"); got != nil {
		t.Fatalf("node asked as claude: %q", got)
	}
}
