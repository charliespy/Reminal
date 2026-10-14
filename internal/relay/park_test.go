// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package relay

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"reminal/internal/protocol"
)

// A parked viewer socket (a viewer switched to another session, keeping
// this one warm) gets nothing of the agent's, says nothing to it, is not
// counted, and is closed when the agent leaves or comes back.

type parkRig struct {
	t   *testing.T
	url string
	in  map[*websocket.Conn]chan protocol.Message
}

// newParkRig runs against this package's relay, or against the relay at
// REMINAL_TEST_RELAY (ws://host:port/ws — the Worker under wrangler dev),
// so both relays are held to the same behaviour.
func newParkRig(t *testing.T) *parkRig {
	t.Helper()
	g := &parkRig{t: t, in: map[*websocket.Conn]chan protocol.Message{}}
	if base := os.Getenv("REMINAL_TEST_RELAY"); base != "" {
		var b [6]byte
		_, _ = rand.Read(b[:])
		g.url = strings.TrimRight(base, "/") + "/PK" + strings.ToUpper(hex.EncodeToString(b[:])) + "/"
		return g
	}
	s := NewServer()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		s.HandleSessionWS(w, r, parts[0], parts[1])
	}))
	t.Cleanup(srv.Close)
	g.url = "ws" + strings.TrimPrefix(srv.URL, "http") + "/PARKSESS/"
	return g
}

// open dials and reads the socket on a goroutine of its own (a gorilla read
// that times out leaves the connection unusable): what arrives is queued,
// and the queue is closed when the socket is.
func (g *parkRig) open(role string) *websocket.Conn {
	g.t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(g.url+role, nil)
	if err != nil {
		g.t.Fatalf("dial %s: %v", role, err)
	}
	g.t.Cleanup(func() { c.Close() })
	ch := make(chan protocol.Message, 64)
	g.in[c] = ch
	go func() {
		defer close(ch)
		for {
			var m protocol.Message
			if err := c.ReadJSON(&m); err != nil {
				return
			}
			ch <- m
		}
	}()
	return c
}

func (g *parkRig) dial(role string, auth protocol.Message) *websocket.Conn {
	g.t.Helper()
	c := g.open(role)
	if err := c.WriteJSON(auth); err != nil {
		g.t.Fatal(err)
	}
	g.want(c, protocol.TypeAuthOK)
	return c
}

func (g *parkRig) agent() *websocket.Conn {
	return g.dial("agent", protocol.Message{Type: protocol.TypeAuth, Token: "AGENT-TOKEN"})
}

func (g *parkRig) viewer() *websocket.Conn {
	return g.dial("viewer", protocol.Message{Type: protocol.TypeAuth})
}

// want takes messages until one of type typ, within the test's own abort cap.
func (g *parkRig) want(c *websocket.Conn, typ protocol.MessageType) protocol.Message {
	g.t.Helper()
	cap := time.After(3 * time.Second)
	for {
		select {
		case m, ok := <-g.in[c]:
			if !ok {
				g.t.Fatalf("closed while waiting for %s", typ)
			}
			if m.Type == typ {
				return m
			}
		case <-cap:
			g.t.Fatalf("no %s", typ)
		}
	}
}

// none says nothing arrives on c for a moment: the window is the assertion.
func (g *parkRig) none(c *websocket.Conn, what string) {
	g.t.Helper()
	select {
	case m, ok := <-g.in[c]:
		if ok {
			g.t.Fatalf("%s: got %s", what, m.Type)
		}
		g.t.Fatalf("%s: closed", what)
	case <-time.After(300 * time.Millisecond):
	}
}

// closed says the relay closed c.
func (g *parkRig) closed(c *websocket.Conn, what string) {
	g.t.Helper()
	cap := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-g.in[c]:
			if !ok {
				return
			}
		case <-cap:
			g.t.Fatalf("%s: still open", what)
		}
	}
}

func TestParkedViewerGetsNothingAndIsNotCounted(t *testing.T) {
	g := newParkRig(t)
	a := g.agent()
	v1 := g.viewer()
	if m := g.want(a, protocol.TypeConnected); m.Count != 1 {
		t.Fatalf("count %d, want 1", m.Count)
	}
	v2 := g.viewer()
	g.want(a, protocol.TypeConnected)

	_ = v1.WriteJSON(protocol.Message{Type: protocol.TypePark})
	g.want(v1, protocol.TypeParked)
	if m := g.want(a, protocol.TypeClosed); m.Count != 1 {
		t.Fatalf("after park the agent counts %d viewers, want 1", m.Count)
	}

	_ = a.WriteJSON(protocol.Message{Type: protocol.TypeData, Data: "output"})
	g.want(v2, protocol.TypeData)
	g.none(v1, "a parked viewer was sent the agent's output")

	_ = v1.WriteJSON(protocol.Message{Type: protocol.TypeData, Data: "keys"})
	_ = v1.WriteJSON(protocol.Message{Type: protocol.TypeResume})
	g.none(a, "a parked viewer's frames reached the agent")

	_ = v1.WriteJSON(protocol.Message{Type: protocol.TypeUnpark})
	g.want(v1, protocol.TypeUnparked)
	if m := g.want(a, protocol.TypeConnected); m.Count != 2 {
		t.Fatalf("after unpark the agent counts %d viewers, want 2", m.Count)
	}
	_ = a.WriteJSON(protocol.Message{Type: protocol.TypeData, Data: "more"})
	g.want(v1, protocol.TypeData)
}

func TestParkedViewerIsClosedWhenTheAgentLeaves(t *testing.T) {
	g := newParkRig(t)
	a := g.agent()
	v := g.viewer()
	live := g.viewer()
	_ = v.WriteJSON(protocol.Message{Type: protocol.TypePark})
	g.want(v, protocol.TypeParked)
	a.Close()
	g.closed(v, "a parked socket outlived its agent")
	g.want(live, protocol.TypeAgentOffline) // a live one is told, and stays
	_ = live.WriteJSON(protocol.Message{Type: protocol.TypeUnpark})
	if m := g.want(live, protocol.TypeParkRefused); m.Error == "" {
		t.Fatal("refused without a reason")
	}
}

func TestParkedViewerIsClosedWhenTheAgentComesBack(t *testing.T) {
	g := newParkRig(t)
	g.agent()
	v := g.viewer()
	_ = v.WriteJSON(protocol.Message{Type: protocol.TypePark})
	g.want(v, protocol.TypeParked)
	g.agent() // a new agent connection (a restart): it supersedes the old
	g.closed(v, "a parked socket survived a new agent, whose key it does not hold")
}

func TestUnauthedSocketCannotPark(t *testing.T) {
	g := newParkRig(t)
	g.agent()
	c := g.open("viewer")
	_ = c.WriteJSON(protocol.Message{Type: protocol.TypePark})
	if m := g.want(c, protocol.TypeError); m.Error == "" {
		t.Fatal("no error")
	}
}

func TestParkIsPaced(t *testing.T) {
	g := newParkRig(t)
	g.agent()
	v := g.viewer()
	refused := false
	for i := 0; i < parkBurst+5 && !refused; i++ {
		typ := protocol.TypePark
		if i%2 == 1 {
			typ = protocol.TypeUnpark
		}
		_ = v.WriteJSON(protocol.Message{Type: typ})
		for answered := false; !answered; {
			select {
			case m := <-g.in[v]:
				switch m.Type {
				case protocol.TypeParkRefused:
					refused, answered = true, true
				case protocol.TypeParked, protocol.TypeUnparked:
					answered = true
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no answer to a park")
			}
		}
	}
	if !refused {
		t.Fatalf("%d parks and unparks at once, none refused", parkBurst+5)
	}
}
