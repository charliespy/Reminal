// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"testing"
	"time"
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
	fp := func() string { of, _ := loadOwners(); return effectiveOwnersFingerprint(of) }
	before := fp()
	if before == "" || fp() != before {
		t.Fatal("no stable fingerprint of the owner set")
	}
	if err := RevokeSelf(a); err != nil {
		t.Fatal(err)
	}
	afterSelf := fp()
	if afterSelf == before {
		t.Fatal("a self-revoke left the owner set's fingerprint as it was: the machine channel would keep the device")
	}
	if _, _, err := RemoveOwner(ownerID(b)); err != nil {
		t.Fatal(err)
	}
	if fp() == afterSelf {
		t.Fatal("a revoke left the owner set's fingerprint as it was")
	}
}

// A device added after a session began, and revoked again, leaves the owner
// set as it was — but had its chance to connect: the session must still
// rekey. And a session begun after the revoke has nothing to do.
func TestOwnersChangedSinceSeesAnAddThenARevoke(t *testing.T) {
	rekeyTestHome(t)
	newOwner(t)
	time.Sleep(1100 * time.Millisecond) // into the next whole second: the write above is before it
	began := time.Now()
	if ownersChangedSince(began) {
		t.Fatal("changed with nothing written since")
	}
	d := newOwner(t)
	if _, _, err := RemoveOwner(ownerID(d)); err != nil {
		t.Fatal(err)
	}
	if !ownersChangedSince(began) {
		t.Fatal("an owner added and revoked since the session began went unnoticed")
	}
	time.Sleep(1100 * time.Millisecond)
	if ownersChangedSince(time.Now()) {
		t.Fatal("a session begun after the revoke would restart again")
	}
	if err := RevokeSelf(d); err != nil {
		t.Fatal(err)
	}
	if !ownersChangedSince(began) {
		t.Fatal("a self-revoke since went unnoticed")
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
	time.Sleep(1100 * time.Millisecond)
	a := &Agent{ownersAt: time.Now()}
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
	a := &Agent{}          // began before every write: a change since
	a.rekeying.Store(true) // one under way: the next waits for the new process
	if got := rekeyReply(t, a); got != "ok restarting\n" {
		t.Fatalf("a second rekey during a restart: %q", got)
	}
}

// A write in the same whole second as the session began counts: with
// whole-second file times (HFS+) it reads as that second, not after it.
func TestOwnersChangedSinceCountsAWriteInTheSameSecond(t *testing.T) {
	rekeyTestHome(t)
	d := newOwner(t)
	p, err := ownersPath()
	if err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if _, _, err := RemoveOwner(ownerID(d)); err != nil {
		t.Fatal(err)
	}
	// As a whole-second filesystem would keep it: the second it was in.
	sec := began.Truncate(time.Second)
	if err := os.Chtimes(p, sec, sec); err != nil {
		t.Fatal(err)
	}
	if !ownersChangedSince(began) {
		t.Fatal("a revoke written in the second the session began went unnoticed")
	}
}
