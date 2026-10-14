// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"reminal/internal/crypto"
	"reminal/internal/protocol"
)

// A restored session is reachable before its scrollback is replayed (Run);
// a viewer that connects then gets its key at once — a handshake needs no
// screen — and only what needs the screen waits for the replay. A viewer
// held seconds for its key gives up: an owner was told "Couldn't connect as
// an owner" over an empty terminal, a long scrollback after a restart.
func TestAHandshakeIsAnsweredWhileTheScrollbackIsReplayed(t *testing.T) {
	a := pakeAgent(t)
	a.replayDone = make(chan struct{}) // the replay under way, for the whole test
	replies := make(chan protocol.Message, 8)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		ex, exID, _ := crypto.NewExID()
		_, mine, _ := crypto.NewCPace([]byte(a.pin), []byte(pakeChannel), pakeSID(a.sessionID, exID), true)
		_ = c.WriteJSON(protocol.Message{Type: protocol.TypePakeInit, ExID: ex, Data: base64.StdEncoding.EncodeToString(mine)})
		// And the resume a viewer sends right behind it: its screen waits for
		// the replay — nothing of the session is sent before it is whole.
		_ = c.WriteJSON(protocol.Message{Type: protocol.TypeResume})
		for {
			var m protocol.Message
			if err := c.ReadJSON(&m); err != nil {
				return
			}
			replies <- m
		}
	}))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	shellExit := make(chan struct{})
	served := make(chan error, 1)
	go func() { served <- a.serveConn(conn, shellExit, false) }()
	t.Cleanup(func() {
		close(shellExit)
		<-served
		close(a.replayDone) // what waited for it goes on, and ends with the connection
		conn.Close()
	})

	deadline := time.After(3 * time.Second)
	for got := false; !got; {
		select {
		case m := <-replies:
			got = m.Type == protocol.TypePakeResp
		case err := <-served:
			t.Fatalf("the connection ended while the scrollback was replayed: %v", err)
		case <-deadline:
			t.Fatal("no answer to the handshake while the scrollback was replayed: the viewer waits on it, and gives up")
		}
	}
	select {
	case m := <-replies:
		t.Fatalf("sent %s while the scrollback was still being replayed", m.Type)
	case err := <-served:
		t.Fatalf("the connection ended while the scrollback was replayed: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
}
