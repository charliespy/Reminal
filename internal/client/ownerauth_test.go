// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"reminal/internal/crypto"
)

// signAction builds the proof a viewer would send.
func signAction(t *testing.T, priv ed25519.PrivateKey, sid, action string, unix int64) ownerProof {
	t.Helper()
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sig := crypto.SignOwner(priv, crypto.OwnerActionTranscript(sid, action, nonce, unix))
	return ownerProof{
		DevicePub: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
		DeviceSig: base64.StdEncoding.EncodeToString(sig),
		Nonce:     base64.StdEncoding.EncodeToString(nonce),
		Unix:      unix,
	}
}

// The threat this exists for: every viewer on a session shares one encryption
// key, so a PIN guest can craft the upgrade message byte for byte. Hiding the
// button changes nothing. The host must refuse.
func TestUpgradeRequiresAnEnrolledOwner(t *testing.T) {
	isolateHome(t)
	a := &Agent{sessionID: "SESSION1"}
	now := time.Now().Unix()

	// A guest with a perfectly well-formed key that is simply not enrolled.
	guestPub, guestPriv, _ := ed25519.GenerateKey(rand.Reader)
	_ = guestPub
	if why := a.verifyOwnerAction(signAction(t, guestPriv, "SESSION1", "upgrade", now), "upgrade"); why == "" {
		t.Fatal("a non-owner device authorised an upgrade — a PIN guest could replace the binary and restart every session")
	}

	// No proof at all, which is what a hand-rolled message looks like.
	if why := a.verifyOwnerAction(ownerProof{}, "upgrade"); why == "" {
		t.Error("an empty proof authorised an upgrade")
	}

	// Now enrol the device and it should pass.
	ownerPub, ownerPriv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(ownerPub), "test device"); err != nil {
		t.Skipf("cannot enrol an owner in this environment: %v", err)
	}
	if why := a.verifyOwnerAction(signAction(t, ownerPriv, "SESSION1", "upgrade", now), "upgrade"); why != "" {
		t.Fatalf("an enrolled owner was refused: %s", why)
	}
}

func TestOwnerProofCannotBeReplayedOrRetargeted(t *testing.T) {
	isolateHome(t)
	ownerPub, ownerPriv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(ownerPub), "test device"); err != nil {
		t.Skipf("cannot enrol an owner in this environment: %v", err)
	}
	a := &Agent{sessionID: "SESSION1"}
	now := time.Now().Unix()

	// Replay: a guest holds the same session key, so it can DECRYPT an owner's
	// request and send it again. The nonce must be single-use.
	p := signAction(t, ownerPriv, "SESSION1", "upgrade", now)
	if why := a.verifyOwnerAction(p, "upgrade"); why != "" {
		t.Fatalf("first use refused: %s", why)
	}
	if why := a.verifyOwnerAction(p, "upgrade"); why == "" {
		t.Error("the same proof worked twice — a guest could replay an owner's upgrade")
	}

	// Wrong session: a proof captured on one session must not authorise another.
	other := &Agent{sessionID: "SESSION2"}
	if why := other.verifyOwnerAction(signAction(t, ownerPriv, "SESSION1", "upgrade", now), "upgrade"); why == "" {
		t.Error("a proof for SESSION1 authorised an action on SESSION2")
	}

	// Wrong action: an "upgrade" proof must not authorise some other verb.
	if why := a.verifyOwnerAction(signAction(t, ownerPriv, "SESSION1", "upgrade", now), "wipe"); why == "" {
		t.Error("an upgrade proof authorised a different action")
	}

	// Stale: bounds how long a captured proof stays interesting.
	old := time.Now().Add(-ownerActionSkew - time.Minute).Unix()
	if why := a.verifyOwnerAction(signAction(t, ownerPriv, "SESSION1", "upgrade", old), "upgrade"); why == "" {
		t.Error("a stale proof was accepted")
	}

	// Tampered signature.
	bad := signAction(t, ownerPriv, "SESSION1", "upgrade", time.Now().Unix())
	bad.DeviceSig = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if why := a.verifyOwnerAction(bad, "upgrade"); why == "" {
		t.Error("a zeroed signature verified")
	}
}

// A forged proof must not be able to consume nonce capacity and lock out the
// real owner.
func TestForgedProofDoesNotSpendANonce(t *testing.T) {
	isolateHome(t)
	ownerPub, ownerPriv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(ownerPub), "test device"); err != nil {
		t.Skipf("cannot enrol an owner in this environment: %v", err)
	}
	a := &Agent{sessionID: "S"}
	now := time.Now().Unix()
	good := signAction(t, ownerPriv, "S", "upgrade", now)

	// Same nonce, but signed by a stranger: rejected, and must NOT be recorded.
	_, guestPriv, _ := ed25519.GenerateKey(rand.Reader)
	forged := signAction(t, guestPriv, "S", "upgrade", now)
	forged.Nonce = good.Nonce
	if why := a.verifyOwnerAction(forged, "upgrade"); why == "" {
		t.Fatal("a forged proof verified")
	}
	if why := a.verifyOwnerAction(good, "upgrade"); why != "" {
		t.Errorf("the real owner was locked out by a forged proof reusing its nonce: %s", why)
	}
}

// A daemon is restarted by every upgrade, inside the window a proof stays
// fresh. The machine channel keeps the proofs it took on disk, so one taken
// before the restart is not taken again after it.
func TestAProofReplayedAfterARestartIsRefused(t *testing.T) {
	isolateHome(t)
	ownerPub, ownerPriv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(ownerPub), "test device"); err != nil {
		t.Skipf("cannot enrol an owner in this environment: %v", err)
	}
	file := ownerNoncesFile()
	before := &Agent{sessionID: "DIRECTORY1", ownerNonces: ownerNonces{file: file}}
	p := signAction(t, ownerPriv, "DIRECTORY1", "integrate", time.Now().Unix())
	if why := before.verifyOwnerAction(p, "integrate"); why != "" {
		t.Fatalf("first use refused: %s", why)
	}
	after := &Agent{sessionID: "DIRECTORY1", ownerNonces: ownerNonces{file: file}}
	if why := after.verifyOwnerAction(p, "integrate"); why == "" {
		t.Fatal("a proof taken before a restart was taken again after it")
	}
	// The control: a fresh proof after the restart is fine.
	if why := after.verifyOwnerAction(signAction(t, ownerPriv, "DIRECTORY1", "integrate", time.Now().Unix()), "integrate"); why != "" {
		t.Fatalf("a fresh proof after the restart was refused: %s", why)
	}
}

// What is on disk is pruned past the freshness window, so the file never
// grows with proofs that could no longer be replayed anyway.
func TestTheProofsOnDiskArePrunedPastTheWindow(t *testing.T) {
	isolateHome(t)
	file := ownerNoncesFile()
	n := &ownerNonces{file: file}
	long := time.Now().Add(-ownerActionSkew * 3)
	if why := n.use("dev", "old", long); why != "" {
		t.Fatal(why)
	}
	fresh := &ownerNonces{file: file}
	if why := fresh.use("dev", "new", time.Now()); why != "" {
		t.Fatal(why)
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var on map[string]int64
	if err := json.Unmarshal(b, &on); err != nil {
		t.Fatal(err)
	}
	if _, kept := on["dev|old"]; kept || len(on) != 1 {
		t.Fatalf("on disk after the window: %v", on)
	}
}

// A process that cannot take the lock does not take the proof: one taken
// without being written down could be taken again after a restart.
func TestAProofIsNotTakenWithoutTheLock(t *testing.T) {
	isolateHome(t)
	lock, held, err := tryLockFile(ownerNoncesLock)
	if err != nil || !held {
		t.Skipf("cannot take the lock in this environment: %v %v", held, err)
	}
	defer unlockFile(lock)
	// flock is per open file description: a second open in this process
	// contends as another process would.
	n := &ownerNonces{file: ownerNoncesFile()}
	if why := n.use("dev", "x", time.Now()); why == "" {
		t.Fatal("a proof was taken while another holder had the lock")
	}
}

// One signed nonce has one spelling: its base64 with a line break in it, or
// unused trailing bits set, decodes to the same bytes, and is refused rather
// than taken as a new proof.
func TestAProofsNonceIsTakenInOneSpellingOnly(t *testing.T) {
	isolateHome(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(pub), "test device"); err != nil {
		t.Fatal(err)
	}
	a := &Agent{sessionID: "SESSION1"}
	p := signAction(t, priv, "SESSION1", "upgrade", time.Now().Unix())
	if why := a.verifyOwnerAction(p, "upgrade"); why != "" {
		t.Fatalf("first use refused: %s", why)
	}
	for i := 1; i <= 5; i++ {
		q := p
		q.Nonce = p.Nonce[:i] + "\n" + p.Nonce[i:]
		if why := a.verifyOwnerAction(q, "upgrade"); why == "" {
			t.Fatalf("the same proof was taken again with its nonce spelled %q", q.Nonce)
		}
	}
}

// A flood of one proof's other spellings takes nothing, and one device that
// has spent its whole allowance in the window keeps only itself waiting:
// another owner's proof is still taken.
func TestOneDeviceCannotFillTheSetForAnother(t *testing.T) {
	isolateHome(t)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, privB, _ := ed25519.GenerateKey(rand.Reader)
	for _, pub := range []ed25519.PublicKey{pubA, pubB} {
		if _, _, err := AddOwner(ownerID(pub), "test device"); err != nil {
			t.Fatal(err)
		}
	}
	a := &Agent{sessionID: "S"}
	p := signAction(t, privA, "S", "upgrade", time.Now().Unix())
	for i := 0; i < ownerNonceMax+5; i++ {
		q := p
		q.Nonce = strings.Repeat("\n", i) + p.Nonce
		_ = a.verifyOwnerAction(q, "upgrade")
	}
	if why := a.verifyOwnerAction(signAction(t, privA, "S", "upgrade", time.Now().Unix()), "upgrade"); why != "" {
		t.Fatalf("other spellings of one proof used up its device's allowance: %s", why)
	}
	// Device A spends all it may in the window...
	now := time.Now()
	devA := base64.StdEncoding.EncodeToString(pubA)
	for i := 0; i < ownerNonceMax; i++ {
		_ = a.ownerNonces.use(devA, fmt.Sprintf("n%04d", i), now)
	}
	if why := a.verifyOwnerAction(signAction(t, privA, "S", "upgrade", now.Unix()), "upgrade"); why == "" {
		t.Fatal("a device past its allowance was let through")
	}
	// ...and B, another owner, is not kept out by it.
	if why := a.verifyOwnerAction(signAction(t, privB, "S", "upgrade", now.Unix()), "upgrade"); why != "" {
		t.Fatalf("another owner was refused because one device filled the set: %s", why)
	}
}
