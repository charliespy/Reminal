// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reminal/internal/protocol"
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
// read as the machine being unreachable, none left unanswered by the
// machine, and every session asked for is started.
//
// Nothing here is timed: on a loaded runner one answer can take longer than
// a call's usual DirectoryTimeout, and that is slowness, not a request
// dropped. Each call is given hammerAbort, a cap only for a run that has
// hung; what is checked is the events — every call returned with its
// answer, and the machine's own count of requests it left unanswered is 0.
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
	// Up once the relay knows the channel: until then it answers that
	// nothing serves it, and the question is asked again.
	deadline := time.Now().Add(hammerAbort)
	for {
		_, qerr := QueryDirectory(machinePub, hammerAbort)
		if qerr == nil {
			break
		}
		if !errors.Is(qerr, ErrDirUnreachable) || time.Now().After(deadline) {
			t.Fatalf("the machine never came up: %v", qerr)
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
				if _, err := QueryDirectory(machinePub, hammerAbort); err != nil {
					fail(err)
				}
			}()
		}
		for i := 0; i < spawns; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := SpawnOnMachine(machinePub, "s", "", hammerAbort); err != nil {
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
	if n := agent.dirUnanswered.Load(); n != 0 {
		t.Errorf("the machine left %d requests unanswered (over even its busy allowance)", n)
	}
	if n := spawned.Load(); n != clients*spawns {
		t.Errorf("%d sessions started, want %d", n, clients*spawns)
	}
}

// hammerAbort ends a hammering test that has hung; it is not how long an
// answer may take.
const hammerAbort = 2 * time.Minute

// own_busy travels in the clear, so its retry_ms could say anything: a call
// waits what a busy answer asks, but never past dirBusyMaxWait.
func TestABusyWaitIsCapped(t *testing.T) {
	for _, c := range []struct {
		ms   int
		want time.Duration
	}{{0, time.Millisecond}, {250, 250 * time.Millisecond}, {1<<31 - 1, dirBusyMaxWait}} {
		var busy *dirBusyError
		if !errors.As(busyAfter(c.ms), &busy) || busy.Wait != c.want {
			t.Errorf("busyAfter(%d) waits %v, want %v", c.ms, busy.Wait, c.want)
		}
	}
}

// The count the hammering test reads is the real one: a request over even
// the busy allowance is left unanswered, and counted.
func TestARequestOverTheBusyAllowanceIsCounted(t *testing.T) {
	a := &Agent{dirLimits: newDirLimits()}
	a.dirLimits.busy = newTokenBucket(0, 0) // spent, and never refilled
	a.dirBusy(nil, protocol.TypeDirQuery, "", time.Second)
	if n := a.dirUnanswered.Load(); n != 1 {
		t.Errorf("unanswered = %d, want 1", n)
	}
}
