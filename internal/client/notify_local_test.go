// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"reminal/internal/crypto"
	"reminal/internal/protocol"
)

// wsPair is a connected pair of websockets: the agent's end and the
// viewer's.
func wsPair(t *testing.T) (agentEnd, viewerEnd *websocket.Conn) {
	t.Helper()
	got := make(chan *websocket.Conn, 1)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		got <- c
	}))
	t.Cleanup(srv.Close)
	v, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	a := <-got
	t.Cleanup(func() { a.Close() })
	return a, v
}

// notifyOn reads what reaches viewer end v until a notify, and returns its
// message ("" if none in time).
func notifyOn(t *testing.T, box *crypto.Box, v *websocket.Conn) string {
	t.Helper()
	_ = v.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var m protocol.Message
		if err := v.ReadJSON(&m); err != nil {
			return ""
		}
		if m.Type != protocol.TypeNotify {
			continue
		}
		pt, err := box.Decrypt(m.Data)
		if err != nil {
			t.Fatalf("notify did not decrypt: %v", err)
		}
		var n struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(pt, &n)
		return n.Message
	}
}

// A `reminal notify` reaches every viewer: one through the relay, and one
// attached on this machine over the local socket — which takes no relay
// traffic, and so got none of it (the notify frame itself, not the dim
// "notified viewers" line in the session's output).
func TestANotifyReachesAViewerAttachedHereAndOneThroughTheRelay(t *testing.T) {
	box, _ := crypto.NewBox(make([]byte, 32))
	a := &Agent{box: box, buf: newScrollback(1 << 16)}
	relayEnd, relayViewer := wsPair(t)
	localEnd, localViewer := wsPair(t)
	a.currentConn = relayEnd
	a.addLocalConn(localEnd)
	if err := a.broadcastNotify("hello viewers"); err != nil {
		t.Fatal(err)
	}
	if got := notifyOn(t, box, localViewer); got != "hello viewers" {
		t.Errorf("the viewer attached here got %q", got)
	}
	if got := notifyOn(t, box, relayViewer); got != "hello viewers" {
		t.Errorf("the viewer through the relay got %q", got)
	}
	// Gone from here: no longer written to; with no relay either, said so.
	a.removeLocalConn(localEnd)
	a.currentConn = nil
	if err := a.broadcastNotify("again"); err != errNoViewerConn {
		t.Fatalf("with no viewer connection: %v", err)
	}
}

// Only a viewer attached here, no relay (offline): it still gets it.
func TestANotifyReachesAViewerAttachedHereWithNoRelay(t *testing.T) {
	box, _ := crypto.NewBox(make([]byte, 32))
	a := &Agent{box: box, buf: newScrollback(1 << 16)}
	localEnd, localViewer := wsPair(t)
	a.addLocalConn(localEnd)
	if err := a.broadcastNotify("offline"); err != nil {
		t.Fatal(err)
	}
	if got := notifyOn(t, box, localViewer); got != "offline" {
		t.Errorf("got %q", got)
	}
}

// resizeOn reads what reaches viewer end v until a resize, and returns its
// size (ok false if none in time).
func resizeOn(t *testing.T, box *crypto.Box, v *websocket.Conn) (cols, rows uint16, ok bool) {
	t.Helper()
	_ = v.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var m protocol.Message
		if err := v.ReadJSON(&m); err != nil {
			return 0, 0, false
		}
		if m.Type != protocol.TypeResize {
			continue
		}
		pt, err := box.Decrypt(m.Data)
		if err != nil {
			t.Fatalf("resize did not decrypt: %v", err)
		}
		var s struct {
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		_ = json.Unmarshal(pt, &s)
		return s.Cols, s.Rows, true
	}
}

// When a viewer leaves, the agent forgets every viewer's size and asks the
// rest to report theirs again (a 0x0 resize). A viewer attached on this
// machine must be asked too: one that is not is left out of the next size,
// and the PTY grows past a smaller terminal here.
func TestAResizeReachesAViewerAttachedHereAndOneThroughTheRelay(t *testing.T) {
	box, _ := crypto.NewBox(make([]byte, 32))
	a := &Agent{box: box, buf: newScrollback(1 << 16)}
	relayEnd, relayViewer := wsPair(t)
	localEnd, localViewer := wsPair(t)
	a.currentConn = relayEnd
	a.addLocalConn(localEnd)
	a.broadcastSize(0, 0)
	if c, r, ok := resizeOn(t, box, localViewer); !ok || c != 0 || r != 0 {
		t.Errorf("the viewer attached here got %dx%d (arrived %v), want the 0x0 ask", c, r, ok)
	}
	if c, r, ok := resizeOn(t, box, relayViewer); !ok || c != 0 || r != 0 {
		t.Errorf("the viewer through the relay got %dx%d (arrived %v), want the 0x0 ask", c, r, ok)
	}
	a.broadcastSize(120, 40)
	if c, r, ok := resizeOn(t, box, localViewer); !ok || c != 120 || r != 40 {
		t.Errorf("the viewer attached here got %dx%d (arrived %v), want 120x40", c, r, ok)
	}
}
