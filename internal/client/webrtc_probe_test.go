// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// A channel's probe clock starts when a stream first asks for it, not when it
// opens: a viewer with no window open sends no frames, so nothing can confirm
// its channel, and timed from the open a healthy channel was already stale
// the moment the viewer opened a window after a pause longer than
// rtcProbeWindow — closed there and then, the window put on the relay for the
// viewer's retry cooldown and a new handshake. One probed past the window
// without a frame acked over it is still closed.
func TestAPeersProbeStartsWhenAWindowAsksNotWhenItOpens(t *testing.T) {
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	p := &rtcPeer{pc: pc}
	p.startProbe()
	p.open.Store(true)
	p.mu.Lock()
	started := !p.openedAt.IsZero()
	p.mu.Unlock()
	if started {
		t.Fatal("the probe clock started as the channel opened, before anything was sent over it")
	}
	a := &Agent{rtcPeers: map[string]*rtcPeer{"v": p}}
	if _, probing, _ := a.rtcSinks(); len(probing) != 1 {
		t.Fatalf("an unprobed open channel was not offered for probing: %d", len(probing))
	}
	if age := p.probeAge(); age > time.Second {
		t.Fatalf("the first ask did not start the probe clock (age %v)", age)
	}
	// Probed past the window, nothing confirmed over it: closed.
	p.mu.Lock()
	p.openedAt = time.Now().Add(-rtcProbeWindow - time.Second)
	p.mu.Unlock()
	if _, probing, _ := a.rtcSinks(); len(probing) != 0 || a.rtcPeers["v"] != nil {
		t.Fatal("a channel probed past the window without a confirmed frame was kept")
	}
}
