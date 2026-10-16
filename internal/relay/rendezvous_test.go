// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// The local relay pairs a copy with a paste by code, passes their frames
// untouched, and spends the code: one paste per offer, none after.
func TestRendezvousPairsOnceByCode(t *testing.T) {
	z := NewRendezvous()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rv/{code}/{role}", func(w http.ResponseWriter, r *http.Request) {
		z.HandleWS(w, r, r.PathValue("code"), r.PathValue("role"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	dial := func(code, role string) *websocket.Conn {
		t.Helper()
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/rv/"+code+"/"+role, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { c.Close() })
		return c
	}
	read := func(c *websocket.Conn) (int, string) {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		mt, b, err := c.ReadMessage()
		if err != nil {
			return -1, err.Error()
		}
		return mt, string(b)
	}

	if _, got := read(dial("NOSOURCE", "paste")); !strings.Contains(got, "too old or invalid") {
		t.Fatalf("a paste with no source: %q", got)
	}
	src := dial("ABCD1234", "source")
	time.Sleep(50 * time.Millisecond)
	dst := dial("ABCD1234", "paste")
	time.Sleep(50 * time.Millisecond)
	_ = src.WriteMessage(websocket.TextMessage, []byte(`{"type":"kex","k":"abc"}`))
	if mt, got := read(dst); mt != websocket.TextMessage || got != `{"type":"kex","k":"abc"}` {
		t.Fatalf("source → paste: %d %q", mt, got)
	}
	_ = dst.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3})
	if mt, got := read(src); mt != websocket.BinaryMessage || got != "\x01\x02\x03" {
		t.Fatalf("paste → source: %d %q", mt, got)
	}
	if _, got := read(dial("ABCD1234", "paste")); !strings.Contains(got, "in progress") && !strings.Contains(got, "too old") {
		t.Fatalf("a second paste on a paired code: %q", got)
	}
	src.Close()
	time.Sleep(100 * time.Millisecond)
	if _, got := read(dial("ABCD1234", "source")); !strings.Contains(got, "too old or invalid") {
		t.Fatalf("a spent code was offered again: %q", got)
	}
}

// A paste sends its first message as soon as it connects. Turned away (a
// spent or unknown code), it must still read why: the relay may not close
// with that message unread, or the reset that follows drops the reason
// before the paste reads it (Windows does).
func TestRendezvousRejectIsReadable(t *testing.T) {
	z := NewRendezvous()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rv/{code}/{role}", func(w http.ResponseWriter, r *http.Request) {
		z.HandleWS(w, r, r.PathValue("code"), r.PathValue("role"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for i := 0; i < 20; i++ {
		c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/rv/NOSOURCE/paste", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"pake_init","data":"`+strings.Repeat("A", 2000)+`"}`)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond) // the relay has answered and closed by now
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, b, err := c.ReadMessage()
		c.Close()
		if err != nil || !strings.Contains(string(b), "too old or invalid") {
			t.Fatalf("try %d: the reason was lost: %q, %v", i, b, err)
		}
	}
}
