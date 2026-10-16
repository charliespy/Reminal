// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package client

import (
	"bufio"
	"encoding/json"
	"os"
	"runtime"
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
	tn := &Tunnel{sessionID: "ABCDEFGH", port: 3000, handshakeFD: handshakeFDOf(t, w)}
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

// A handshake closes its own fd and nothing else: files opened after it,
// on the numbers it freed, are still open once the collector has run.
// (Both handshake tests once left the pipe's write end a second owner of
// the fd, whose finaliser closed other tests' sockets and files.)
func TestTunnelHandshakeClosesOnlyItsOwnFD(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		func() {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			tn := &Tunnel{sessionID: "ABCDEFGH", port: 3000, handshakeFD: handshakeFDOf(t, w), webURL: "http://127.0.0.1:1"}
			tn.abandon = func() {}
			if delivered {
				tn.writeHandshake()
			} else {
				tn.setLastErr("TLS handshake failed")
				tn.writeHandshakeError()
			}
		}()
		var later []*os.File
		for i := 0; i < 4; i++ {
			f, err := os.CreateTemp(t.TempDir(), "later")
			if err != nil {
				t.Fatal(err)
			}
			later = append(later, f)
		}
		collectAndClean()
		for _, f := range later {
			if _, err := f.Write([]byte("x")); err != nil {
				t.Errorf("delivered=%v: a file opened after the handshake was closed under it: %v", delivered, err)
			}
			f.Close()
		}
	}
}

// collectAndClean runs the collector and waits until the cleanups it queued
// (os.File's close among them) have had their turn: twice, so a cleanup
// queued behind the sentinel's is run before the second sentinel's.
func collectAndClean() {
	for i := 0; i < 2; i++ {
		done := make(chan struct{})
		// [64]byte: not tiny-allocated, so it is freed on its own.
		runtime.AddCleanup(new([64]byte), func(ch chan struct{}) { close(ch) }, done)
		runtime.GC()
		<-done
	}
}
