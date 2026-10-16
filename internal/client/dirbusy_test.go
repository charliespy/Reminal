// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A bucket that refuses says how long until it would take one: what a
// caller over the allowance is told to wait.
func TestABucketSaysWhenItWouldTakeOne(t *testing.T) {
	now := time.Now()
	tb := newTokenBucket(2, 4) // 2 at once, then one each 250 ms
	tb.last = now
	for i := 0; i < 2; i++ {
		if ok, _ := tb.take(now); !ok {
			t.Fatalf("take %d refused within the burst", i)
		}
	}
	ok, wait := tb.take(now)
	if ok || wait != 250*time.Millisecond {
		t.Fatalf("over the burst: %v, wait %v; want refused, 250ms", ok, wait)
	}
	if ok, _ := tb.take(now.Add(wait)); !ok {
		t.Error("asked again after the wait it gave, refused")
	}
}

// Two clients hammering one machine — two tabs, two browsers, two shells —
// past its allowance for queries and for new sessions: every request is
// answered (busy, then taken when asked again at the time it gave), none
// read as the machine being unreachable, and every session asked for is
// started.
func TestTwoClientsHammeringOneMachineAreAllAnswered(t *testing.T) {
	isolateHome(t)
	startTestRelay(t)
	id, err := MyOwnerID()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddOwner(id, "self"); err != nil {
		t.Fatal(err)
	}
	machinePub, err := MachinePub()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := NewAgentWith("test", AgentOptions{Machine: true})
	if err != nil {
		t.Fatal(err)
	}
	var spawned atomic.Int32
	agent.spawn = func(name, cwd string) (*SpawnedSession, error) {
		n := spawned.Add(1)
		return &SpawnedSession{ID: "S" + string(rune('A'+n%26)), PIN: "1"}, nil
	}
	stop := make(chan struct{})
	defer close(stop)
	go func() { _ = agent.RunMachine(stop) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, qerr := QueryDirectory(machinePub, DirectoryTimeout); qerr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the machine never came up")
		}
		time.Sleep(100 * time.Millisecond)
	}

	const clients, queries, spawns = 2, 30, 6 // 60 queries past a burst of 32; 12 sessions past 4
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []error
	fail := func(err error) { mu.Lock(); failed = append(failed, err); mu.Unlock() }
	for c := 0; c < clients; c++ {
		for i := 0; i < queries; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := QueryDirectory(machinePub, DirectoryTimeout); err != nil {
					fail(err)
				}
			}()
		}
		for i := 0; i < spawns; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := SpawnOnMachine(machinePub, "s", "", DirectoryTimeout); err != nil {
					fail(err)
				}
			}()
		}
	}
	wg.Wait()
	for _, err := range failed {
		if errors.Is(err, ErrDirUnreachable) {
			t.Errorf("a request read as the machine unreachable: %v", err)
		} else {
			t.Errorf("a request failed: %v", err)
		}
	}
	if n := spawned.Load(); n != clients*spawns {
		t.Errorf("%d sessions started, want %d", n, clients*spawns)
	}
}
