// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"reminal/internal/protocol"
)

// An own_init seen before — the same viewer ephemeral, replayed — is not
// answered and does not get its token back: a replay costs what any unproven
// attempt costs, and opens no window for viewers from before sealed frames.
// Past the remembering window it is treated as any other attempt again.
func TestAReplayedOwnerInitIsNotAnsweredNorRefunded(t *testing.T) {
	isolateHome(t)
	a := ownerAgent(t)
	at := time.Now()
	a.ownClock = func() time.Time { return at }
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(pub), "phone"); err != nil {
		t.Fatal(err)
	}
	msg := ownInit(t, a.sessionID, priv)

	conn, got := ownerConn(t)
	a.handleOwnerInit(conn, msg)
	if reply, ok := nextOwnerMsg(t, got); !ok || reply.Type != protocol.TypeOwnerResp {
		t.Fatalf("the first own_init was not answered: %+v", reply)
	}
	before := a.ownTokens

	conn2, got2 := ownerConn(t)
	a.handleOwnerInit(conn2, msg)
	if reply, ok := nextOwnerMsg(t, got2); ok && reply.Type == protocol.TypeOwnerResp {
		t.Fatal("a replayed own_init was answered")
	}
	if a.ownTokens >= before {
		t.Fatalf("a replayed own_init got its token back: %v then %v", before, a.ownTokens)
	}

	// A fresh one from the same device is answered as ever.
	conn3, got3 := ownerConn(t)
	a.handleOwnerInit(conn3, ownInit(t, a.sessionID, priv))
	if reply, ok := nextOwnerMsg(t, got3); !ok || reply.Type != protocol.TypeOwnerResp {
		t.Fatalf("a fresh own_init after a replay was not answered: %+v", reply)
	}

	// Past the window, the old one is any other attempt (answered, refunded).
	at = at.Add(ownerEphWindow + time.Second)
	conn4, got4 := ownerConn(t)
	a.handleOwnerInit(conn4, msg)
	if reply, ok := nextOwnerMsg(t, got4); !ok || reply.Type != protocol.TypeOwnerResp {
		t.Fatalf("past its window an own_init was still refused: %+v", reply)
	}
}
