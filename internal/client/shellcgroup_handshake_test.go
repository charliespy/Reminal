// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"reminal/internal/pty"
)

// handshakeOf is what an agent with cg writes to the parent of `reminal new`.
func handshakeOf(t *testing.T, cg *pty.ShellCgroup) (SpawnedSession, map[string]json.RawMessage) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a := &Agent{handshakeFD: int(w.Fd()), sessionID: "S1", pin: "12345678", webURL: "http://x", shellCgroup: cg}
	a.writeHandshake()
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var sp SpawnedSession
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &sp); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(line), &raw)
	return sp, raw
}

// The startup handshake says how starting the shell in its cgroup went, and
// says nothing of it when no cgroup was asked for.
func TestTheHandshakeSaysHowTheShellCgroupWent(t *testing.T) {
	sp, _ := handshakeOf(t, &pty.ShellCgroup{Dir: "/tmp/not-a-cgroup", Error: "not a cgroup v2 directory"})
	if cg := sp.ShellCgroup; cg == nil || cg.Dir != "/tmp/not-a-cgroup" || cg.OK || cg.Error != "not a cgroup v2 directory" {
		t.Errorf("handshake shell_cgroup = %+v", cg)
	}
	sp, _ = handshakeOf(t, &pty.ShellCgroup{Dir: "/sys/fs/cgroup/s", OK: true})
	if cg := sp.ShellCgroup; cg == nil || !cg.OK || cg.Error != "" {
		t.Errorf("handshake shell_cgroup = %+v, want ok", cg)
	}
	if _, raw := handshakeOf(t, nil); raw["shell_cgroup"] != nil {
		t.Errorf("shell_cgroup in the handshake with none asked for: %s", raw["shell_cgroup"])
	}
}
