// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"testing"
)

// A revoked device is signed out of open sessions: a session restarts under
// a new key when the owner set it began with has changed — revoked, or
// self-revoked — and only then.

func rekeyTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("REMINAL_OWNERS_DIR", t.TempDir())
}

func newOwner(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddOwner(ownerID(pub), "test"); err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestOwnersFingerprintSeesEveryWayOut(t *testing.T) {
	rekeyTestHome(t)
	a, b := newOwner(t), newOwner(t)
	fp := currentOwnersFingerprint()
	if fp == "" || currentOwnersFingerprint() != fp {
		t.Fatal("no stable fingerprint of the owner set")
	}
	if err := RevokeSelf(a); err != nil {
		t.Fatal(err)
	}
	afterSelf := currentOwnersFingerprint()
	if afterSelf == fp {
		t.Fatal("a self-revoke left the owner set's fingerprint as it was: open sessions would keep the device")
	}
	if _, _, err := RemoveOwner(ownerID(b)); err != nil {
		t.Fatal(err)
	}
	if currentOwnersFingerprint() == afterSelf {
		t.Fatal("a revoke left the owner set's fingerprint as it was")
	}
}

func rekeyReply(t *testing.T, a *Agent) string {
	t.Helper()
	c1, c2 := net.Pipe()
	defer c1.Close()
	go a.handleControlConn(c2)
	if _, err := c1.Write([]byte("rekey\n")); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(c1).ReadString('\n')
	return line
}

func TestRekeyWithTheSameOwnersDoesNotRestart(t *testing.T) {
	rekeyTestHome(t)
	newOwner(t)
	a := &Agent{ownersFP: currentOwnersFingerprint()}
	if got := rekeyReply(t, a); got != "ok unchanged\n" {
		t.Fatalf("rekey with the owner set unchanged: %q, want no restart", got)
	}
	if a.rekeying.Load() {
		t.Fatal("a restart was started")
	}
}

func TestRekeyRestartsOnceWhileOneIsUnderWay(t *testing.T) {
	rekeyTestHome(t)
	newOwner(t)
	a := &Agent{ownersFP: "the owner set this process began with"}
	a.rekeying.Store(true) // one under way: the next waits for the new process
	if got := rekeyReply(t, a); got != "ok restarting\n" {
		t.Fatalf("a second rekey during a restart: %q", got)
	}
}
