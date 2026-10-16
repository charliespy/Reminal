// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

// A forward that couldn't reach the relay tells `reminal expose` why, once,
// and then has nothing left to send: the credentials can't follow an error.
func TestTunnelHandshakeErrorIsSentOnce(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	tn := &Tunnel{sessionID: "ABCDEFGH", port: 3000, handshakeFD: int(w.Fd())}
	tn.setLastErr("TLS handshake failed")
	if !tn.handshakePending() {
		t.Fatal("handshake should be pending before anything is sent")
	}
	if !tn.writeHandshakeError() {
		t.Fatal("writeHandshakeError reported nothing sent")
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var sp SpawnedSession
	if err := json.Unmarshal([]byte(line), &sp); err != nil {
		t.Fatal(err)
	}
	if sp.Error != "TLS handshake failed" || sp.ID != "" {
		t.Fatalf("got %+v, want only the error", sp)
	}
	if tn.handshakePending() || tn.writeHandshakeError() {
		t.Fatal("a second handshake was possible after the error")
	}
	tn.writeHandshake() // must be a no-op now, not a write to a closed fd
}
