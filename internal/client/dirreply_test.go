// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"sync"
	"testing"
	"time"

	"reminal/internal/protocol"
	"reminal/internal/session"
)

// Every owner on a machine's channel hears every answer the machine sends: a
// browser with the machine's card open asks for its listing every few
// seconds, and another agent may be asking too. A request that took the first
// answer to come by read somebody else's listing as its own: keys typed into
// a session reported "too old to accept keystrokes" although they landed, and
// a transcript read reported "too old to dump scrollback" one call and worked
// the next. Each request waits for the answer to it.
func TestADirQueryHearsOnlyItsOwnAnswer(t *testing.T) {
	isolateHome(t)
	startTestRelay(t)

	id, err := MyOwnerID()
	if err != nil {
		t.Fatalf("owner id: %v", err)
	}
	if _, _, err := AddOwner(id, "self"); err != nil {
		t.Fatalf("add owner: %v", err)
	}
	machinePub, err := MachinePub()
	if err != nil {
		t.Fatalf("machine pub: %v", err)
	}
	if err := session.WriteActive(session.Active{
		ID: "PORTWIRE", Kind: "port", Port: 8080, PID: os.Getpid(),
		PIN: "864209", OpenURL: "https://port-portwire.reminal.app/",
	}); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	defer close(stop)
	go runDirectoryHost(stop, true, "test")

	// Up: one plain listing answered.
	deadline := time.Now().Add(6 * time.Second)
	for {
		if _, err := QueryDirectory(machinePub, DirectoryTimeout); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the machine channel never came up")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Another owner on the same channel — a browser with the machine's card
	// open — asking for the listing. It hears what the machine tells every
	// owner, the answer to each new owner's handshake included, and asks the
	// moment it does: its question then races the one that owner asks next.
	noisy, _, err := dialDirectoryOwner(machinePub, 30*time.Second)
	if err != nil {
		t.Fatalf("second owner: %v", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			var m protocol.Message
			if readDir(noisy, &m) != nil {
				return
			}
			if m.Type == protocol.TypeOwnerResp {
				_ = writeDir(noisy, protocol.Message{Type: protocol.TypeDirQuery})
			}
		}
	}()
	defer func() { noisy.Close(); wg.Wait() }()

	wrong := 0
	for i := 0; i < 30; i++ {
		resp, err := queryDirectory(machinePub, DirectoryTimeout, dirQueryReq{OpenID: "PORTWIRE"})
		if err != nil {
			t.Fatalf("open lookup %d: %v", i, err)
		}
		if resp.OpenURL != "https://port-portwire.reminal.app/" {
			wrong++
		}
		// Two questions a lookup (its own and the other owner's), inside the
		// machine's rate limit for them.
		time.Sleep(150 * time.Millisecond)
	}
	if wrong > 0 {
		t.Fatalf("%d of 30 open lookups took another owner's listing for their answer", wrong)
	}
}
