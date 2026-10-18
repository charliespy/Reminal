// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"

	"reminal/internal/crypto"
	"reminal/internal/protocol"
)

// relayCut is what a relay between viewer and agent takes out of a PIN
// handshake: the bound offer (FramesData), the offer itself (Frames), the
// agent's sealed-frames answer (Seal).
type relayCut struct{ framesData, frames, seal bool }

// pinHandshake runs this package's real viewer half of the PIN handshake —
// both elements offered, pakeOpenEither, admitSeal — against the real agent,
// through a relay that cuts what cut says. It returns whether the answer was
// to the bound offer and what admitSeal made of it.
func pinHandshake(t *testing.T, a *Agent, cut relayCut) (bool, error) {
	t.Helper()
	conn, got := ownerConn(t)
	exHex, exID, err := crypto.NewExID()
	if err != nil {
		t.Fatal(err)
	}
	st, mine, _ := crypto.NewCPace([]byte(a.pin), []byte(pakeChannel), pakeSID(a.sessionID, exID, false), true)
	stF, mineF, _ := crypto.NewCPace([]byte(a.pin), []byte(pakeChannel), pakeSID(a.sessionID, exID, true), true)
	framesData, frames := base64.StdEncoding.EncodeToString(mineF), 1
	if cut.framesData {
		framesData = ""
	}
	if cut.frames {
		frames = 0
	}
	a.handlePakeInit(conn, exHex, base64.StdEncoding.EncodeToString(mine), framesData, frames, "")
	reply, ok := nextOwnerMsg(t, got)
	if !ok || reply.Type != protocol.TypePakeResp {
		t.Fatalf("no pake_resp: %+v", reply)
	}
	if cut.seal {
		reply.Seal = ""
	}
	elem, _ := base64.StdEncoding.DecodeString(reply.Data)
	wrapped, _ := base64.StdEncoding.DecodeString(reply.Wrap)
	key, sk, bound, err := pakeOpenEither(stF, st, elem, exID, wrapped)
	if err != nil {
		return false, err
	}
	_, err = admitSeal(sk, key, exID, reply.Seal, bound, "s:"+a.sessionID)
	return bound, err
}

// Offered both ways, this agent answers the bound offer, sealed; the viewer
// takes it and remembers the session as one that seals.
func TestABoundOfferIsAnsweredSealedAndRemembered(t *testing.T) {
	isolateHome(t)
	a := pakeAgent(t)
	bound, err := pinHandshake(t, a, relayCut{})
	if err != nil || !bound {
		t.Fatalf("bound=%v err=%v, want the bound offer answered and taken", bound, err)
	}
	if !sealedSeen("s:" + a.sessionID) {
		t.Fatal("a session seen to seal was not remembered")
	}
}

// The bound answer with its sealed-frames answer taken out: refused — the key
// says the agent saw the offer, so the seal is not missing by age.
func TestARelayStrippingTheSealFromABoundAnswerIsRefused(t *testing.T) {
	isolateHome(t)
	a := pakeAgent(t)
	if _, err := pinHandshake(t, a, relayCut{seal: true}); !errors.Is(err, errSealMissing) {
		t.Fatalf("a bound answer without its seal was taken: %v", err)
	}
}

// Once a session has sealed, a relay that strips the offer (and with it the
// agent's sealed answer) is refused: the downgrade a relay could bring about
// is the one this device has seen is not the agent's own.
func TestARelayDowngradingASessionSeenToSealIsRefused(t *testing.T) {
	isolateHome(t)
	a := pakeAgent(t)
	if _, err := pinHandshake(t, a, relayCut{}); err != nil {
		t.Fatal(err)
	}
	for name, cut := range map[string]relayCut{
		"offer stripped":                    {framesData: true, frames: true},
		"bound offer and the seal stripped": {framesData: true, seal: true},
	} {
		if _, err := pinHandshake(t, a, cut); !errors.Is(err, errSealDowngrade) {
			t.Fatalf("%s: a session seen to seal was taken unsealed: %v", name, err)
		}
	}
	// The bound offer alone stripped, the agent still seals (unbound): taken.
	if bound, err := pinHandshake(t, a, relayCut{framesData: true}); err != nil || bound {
		t.Fatalf("an unbound sealed answer: bound=%v err=%v", bound, err)
	}
}

// A first contact — a session this device has never seen seal — is taken
// unsealed: an agent from before sealed frames still connects. It is the
// residual: a relay could downgrade that first contact. With the strict
// switch on, it is refused too.
func TestAFirstContactUnsealedIsTakenUnlessStrict(t *testing.T) {
	isolateHome(t)
	a := pakeAgent(t)
	cut := relayCut{framesData: true, frames: true}
	if bound, err := pinHandshake(t, a, cut); err != nil || bound {
		t.Fatalf("a first contact unsealed: bound=%v err=%v, want taken", bound, err)
	}
	sealedFramesRequired = true
	t.Cleanup(func() { sealedFramesRequired = false })
	if _, err := pinHandshake(t, a, cut); !errors.Is(err, errSealDowngrade) {
		t.Fatalf("strict, an unsealed answer was taken: %v", err)
	}
}

// A viewer from before sealed frames — one element, no offer — still connects
// to this agent, answered unsealed as before.
func TestAViewerFromBeforeSealedFramesStillConnects(t *testing.T) {
	a := pakeAgent(t)
	conn, got := ownerConn(t)
	exHex, exID, _ := crypto.NewExID()
	st, mine, _ := crypto.NewCPace([]byte(a.pin), []byte(pakeChannel), pakeSID(a.sessionID, exID, false), true)
	a.handlePakeInit(conn, exHex, base64.StdEncoding.EncodeToString(mine), "", 0, "")
	reply, ok := nextOwnerMsg(t, got)
	if !ok || reply.Seal != "" {
		t.Fatalf("an older viewer's answer: %+v", reply)
	}
	elem, _ := base64.StdEncoding.DecodeString(reply.Data)
	k, _ := st.Finish(elem)
	wrapped, _ := base64.StdEncoding.DecodeString(reply.Wrap)
	if _, err := crypto.UnwrapSessionKey(k, exID, wrapped); err != nil {
		t.Fatalf("an older viewer could not connect: %v", err)
	}
}

// ownHandshake runs the owner handshake against the real agent: the device
// signs both transcripts, a relay cuts what cut says (framesData: the bound
// signature), and the viewer's checks (ownerAnswerVerifies, admitSeal) judge
// the answer.
func ownHandshake(t *testing.T, a *Agent, priv ed25519.PrivateKey, cut relayCut) (bool, error) {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	eph, _ := crypto.NewEphemeralKey()
	exHex, exID, _ := crypto.NewExID()
	vEph := eph.PublicKey().Bytes()
	msg := protocol.Message{
		Type:      protocol.TypeOwnerInit,
		ExID:      exHex,
		Data:      base64.StdEncoding.EncodeToString(vEph),
		DevicePub: base64.StdEncoding.EncodeToString(pub),
		DeviceSig: base64.StdEncoding.EncodeToString(crypto.SignOwner(priv, crypto.OwnerClientTranscript(a.sessionID, vEph, pub))),
		FramesSig: base64.StdEncoding.EncodeToString(crypto.SignOwner(priv, crypto.OwnerClientTranscriptFor(a.sessionID, vEph, pub, true))),
		Frames:    1,
	}
	if cut.framesData {
		msg.FramesSig = ""
	}
	if cut.frames {
		msg.Frames = 0
	}
	conn, got := ownerConn(t)
	a.handleOwnerInit(conn, msg)
	reply, ok := nextOwnerMsg(t, got)
	if !ok || reply.Type != protocol.TypeOwnerResp {
		t.Fatalf("no own_resp: %+v", reply)
	}
	if cut.seal {
		reply.Seal = ""
	}
	aEph, _ := base64.StdEncoding.DecodeString(reply.Data)
	machinePub, _ := base64.StdEncoding.DecodeString(reply.MachinePub)
	machineSig, _ := base64.StdEncoding.DecodeString(reply.MachineSig)
	bound, ok := ownerAnswerVerifies(a.sessionID, vEph, aEph, pub, machinePub, machineSig, reply.Seal)
	if !ok {
		return false, errors.New("machine signature invalid")
	}
	peer, _ := crypto.PeerPublicKey(aEph)
	shared, _ := eph.ECDH(peer)
	wrapped, _ := base64.StdEncoding.DecodeString(reply.Wrap)
	sk, err := crypto.UnwrapSessionKey(shared, exID, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admitSeal(sk, shared, exID, reply.Seal, bound, "m:"+base64.StdEncoding.EncodeToString(machinePub))
	return bound, err
}

// The owner path: the bound offer answered sealed, its answer signed in — a
// relay that takes the seal out leaves a signature that verifies over
// neither transcript; one that strips the bound offer from a machine seen to
// seal is refused; a device from before (one signature) is answered as ever.
func TestTheOwnerHandshakeBindsItsSealedFrames(t *testing.T) {
	isolateHome(t)
	a := ownerAgent(t)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(pub), "phone"); err != nil {
		t.Fatal(err)
	}
	if bound, err := ownHandshake(t, a, priv, relayCut{}); err != nil || !bound {
		t.Fatalf("bound=%v err=%v, want the bound offer answered and taken", bound, err)
	}
	if _, err := ownHandshake(t, a, priv, relayCut{seal: true}); err == nil || errors.Is(err, errSealDowngrade) {
		t.Fatalf("the seal taken out of a signed answer: %v, want the signature refused", err)
	}
	if _, err := ownHandshake(t, a, priv, relayCut{framesData: true, frames: true}); !errors.Is(err, errSealDowngrade) {
		t.Fatalf("a machine seen to seal was taken unsealed: %v", err)
	}

	// One signature, no offer: an older device, answered as before.
	conn, got := ownerConn(t)
	old := ownInit(t, a.sessionID, priv)
	a.handleOwnerInit(conn, old)
	if reply, ok := nextOwnerMsg(t, got); !ok || reply.Type != protocol.TypeOwnerResp || reply.Seal != "" {
		t.Fatalf("an older device was not answered as before: %+v", reply)
	}
}
