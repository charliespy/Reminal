// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"testing"
	"time"
)

// TestKexThrottle verifies the agent's kex token bucket: it allows an initial
// burst (so several viewers / a few PIN fat-fingers connect fine) then rate-
// limits, and refills over time. Each answered kex_init is one online PIN
// guess, so this is what bounds a malicious relay's brute-force.
func TestKexThrottle(t *testing.T) {
	a := &Agent{}
	base := time.Unix(1_700_000_000, 0)

	// Burst: the first kexBurst attempts at t0 all pass.
	for i := 0; i < kexBurst; i++ {
		if !a.allowKex(base) {
			t.Fatalf("attempt %d within burst should be allowed", i)
		}
	}
	// The next one (still t0) is throttled — bucket drained.
	if a.allowKex(base) {
		t.Fatalf("attempt past the burst at the same instant should be throttled")
	}

	// After one refill interval, exactly one more token is available.
	if !a.allowKex(base.Add(kexRefill)) {
		t.Fatalf("one token should have refilled after kexRefill")
	}
	if a.allowKex(base.Add(kexRefill)) {
		t.Fatalf("only one token should refill per interval")
	}

	// The bucket never exceeds kexBurst even after a long idle stretch.
	far := base.Add(1000 * kexRefill)
	for i := 0; i < kexBurst; i++ {
		if !a.allowKex(far) {
			t.Fatalf("post-idle burst attempt %d should be allowed", i)
		}
	}
	if a.allowKex(far) {
		t.Fatalf("bucket should be capped at kexBurst even after long idle")
	}
}

// The short-term pace alone let a patient guesser try the PIN thousands of
// times a day. The long-term allowance is what bounds that: once it is spent,
// attempts are answered only as fast as it refills, however long they keep
// coming.
func TestKexLongTermAllowance(t *testing.T) {
	a := &Agent{}
	now := time.Unix(1_700_000_000, 0)

	// Someone trying as fast as the short-term pace allows, for a whole day.
	answered := 0
	for end := now.Add(24 * time.Hour); now.Before(end); now = now.Add(kexRefill) {
		if a.allowKex(now) {
			answered++
		}
	}
	ceiling := kexLongBurst + int((24*time.Hour)/kexLongRefill) + 1
	if answered > ceiling {
		t.Fatalf("answered %d PIN handshakes in a day, want at most %d", answered, ceiling)
	}
	if answered < kexLongBurst {
		t.Fatalf("answered only %d — the allowance should cover at least its burst", answered)
	}

	// And ordinary use is untouched: after a quiet stretch a person can still
	// connect several times in a row.
	now = now.Add(48 * time.Hour)
	for i := 0; i < kexBurst; i++ {
		if !a.allowKex(now) {
			t.Fatalf("connect %d after a quiet stretch was refused", i)
		}
	}
}

// One source on its own gets exactly what the machine-wide allowance gives:
// tagging it never makes it wait longer.
func TestKexSourceShareAloneMatchesMachineWide(t *testing.T) {
	run := func(src string) int {
		a := &Agent{}
		base := time.Now()
		n := 0
		for i := 0; i < 180; i++ { // a handshake a minute for three hours
			if ok, _ := a.allowKexFrom(base.Add(time.Duration(i)*time.Minute), src); ok {
				n++
			}
		}
		return n
	}
	if tagged, untagged := run("A"), run(""); tagged != untagged {
		t.Fatalf("one tagged source got %d handshakes, untagged got %d", tagged, untagged)
	}
}

// Once the machine-wide allowance is low, a source past its share waits and
// another source is still served.
func TestKexSourceShareUnderPressure(t *testing.T) {
	a := &Agent{}
	now := time.Now()
	a.allowKexFrom(now, "B") // B is about too
	for i := 0; i < 7; i++ {
		if ok, _ := a.allowKexFrom(now, "A"); !ok {
			t.Fatalf("A refused at %d within the burst", i)
		}
	}
	// Spread the rest out so the short-term bucket is not what refuses.
	at := now
	for i := 0; i < 20; i++ {
		at = at.Add(kexRefill)
		a.allowKexFrom(at, "A")
	}
	at = at.Add(kexRefill)
	ok, retry := a.allowKexFrom(at, "A")
	if ok || retry <= 0 {
		t.Fatalf("A past its share under pressure: ok=%v retry=%v", ok, retry)
	}
	if ok, _ := a.allowKexFrom(at.Add(kexRefill), "B"); !ok {
		t.Fatal("another source was refused while the machine still had allowance")
	}
}

// Two sources over three hours, one asking every 10 s and one every 3 min:
// the second keeps getting answered, with no long run of refusals. The second
// has proved the PIN: one that never does looks just like a slow guesser, and
// the escalating backoff (TestKexBackoffDoublesUpToAnHour) holds it.
func TestKexSteadySourceKeepsBeingAnswered(t *testing.T) {
	a := &Agent{}
	a.markProvenSource("steady")
	base := time.Now()
	got, longestGap := 0, time.Duration(0)
	last := base
	for sec := 0; sec < 3*3600; sec += 10 {
		now := base.Add(time.Duration(sec) * time.Second)
		a.allowKexFrom(now, "frequent")
		if sec%180 != 0 {
			continue
		}
		if ok, _ := a.allowKexFrom(now, "steady"); ok {
			got++
			last = now
			a.markProvenSource("steady") // and proves it again

		} else if g := now.Sub(last); g > longestGap {
			longestGap = g
		}
	}
	if got < 30 || longestGap > 15*time.Minute {
		t.Fatalf("steady source: %d of 60 answered, longest gap %v", got, longestGap)
	}
}

// A source that keeps asking without ever proving the PIN is answered for the
// long-term allowance, then waits longer each time: 3 minutes, 6, 12, 24, 48,
// then an hour, never more. The terminal is told once.
func TestKexBackoffDoublesUpToAnHour(t *testing.T) {
	a := &Agent{}
	now := time.Unix(1_700_000_000, 0)
	var at []time.Time
	tells := 0
	for end := now.Add(10 * time.Hour); now.Before(end); now = now.Add(time.Second) {
		a.kexMu.Lock()
		ok, _, tell := a.allowKexLocked(now, "guesser")
		a.kexMu.Unlock()
		if ok {
			at = append(at, now)
		}
		if tell {
			tells++
		}
	}
	if len(at) < kexLongBurst+9 {
		t.Fatalf("answered only %d", len(at))
	}
	// at[kexLongBurst] is the first past the allowance: the wait follows it.
	var gaps []string
	for i := kexLongBurst + 1; i < kexLongBurst+9; i++ {
		gaps = append(gaps, at[i].Sub(at[i-1]).Round(time.Minute).String())
	}
	if got, want := strings.Join(gaps, " "), "3m0s 6m0s 12m0s 24m0s 48m0s 1h0m0s 1h0m0s 1h0m0s"; got != want {
		t.Fatalf("waits after the allowance: %s, want %s", got, want)
	}
	if tells != 1 {
		t.Fatalf("the terminal was told %d times, want once", tells)
	}
}

// While unproven sources wait, one that has proved the PIN is answered, and
// its handshakes never count towards the wait.
func TestKexBackoffSparesProvenSources(t *testing.T) {
	a := &Agent{}
	a.markProvenSource("friend")
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 200; i++ {
		a.allowKexFrom(now, "guesser")
		if ok, _ := a.allowKexFrom(now, "friend"); ok {
			a.markProvenSource("friend")
		}
		now = now.Add(kexLongRefill)
	}
	if ok, _ := a.allowKexFrom(now, "newcomer"); ok || a.kexBackoff.wait != kexBackoffMax {
		t.Fatalf("a new source during the backoff: ok=%v, the wait %v", ok, a.kexBackoff.wait)
	}
	if ok, _ := a.allowKexFrom(now.Add(kexLongRefill), "friend"); !ok {
		t.Fatal("a proven source was held by the backoff")
	}
}

// A guesser that never stops gets about one handshake an hour once the
// allowance is spent; pausing between them gains it nothing.
func TestKexBackoffHoldsAPatientGuesser(t *testing.T) {
	for _, pause := range []time.Duration{0, kexBackoffFirst, 10 * time.Minute, time.Hour} {
		a := &Agent{}
		now := time.Unix(1_700_000_000, 0)
		answered := 0
		for end := now.Add(4 * 24 * time.Hour); now.Before(end); {
			ok, retry := a.allowKexFrom(now, "guesser")
			if ok {
				answered++
				now = now.Add(pause + time.Second)
			} else {
				now = now.Add(retry)
			}
		}
		if ceiling := kexLongBurst + 4*24 + 4; answered > ceiling {
			t.Errorf("pausing %v: %d answered in four days, want at most %d", pause, answered, ceiling)
		}
	}
}

// Every hour of quiet after a wait takes one doubling back off; a source that
// comes straight back is not forgiven. A handshake that proves the PIN gives
// its share of the allowance back.
func TestKexBackoffForgivesQuiet(t *testing.T) {
	a := &Agent{}
	now := time.Unix(1_700_000_000, 0)
	for a.kexBackoff.level < 4 {
		if ok, retry := a.allowKexFrom(now, "guesser"); !ok {
			now = now.Add(retry)
		} else {
			now = now.Add(time.Second)
		}
	}
	now = a.kexBackoff.until.Add(time.Minute)
	a.allowKexFrom(now, "guesser")
	if a.kexBackoff.level != 5 {
		t.Fatalf("back after a minute: level %d, want 5", a.kexBackoff.level)
	}
	now = a.kexBackoff.until.Add(2*time.Hour + time.Minute)
	a.allowKexFrom(now, "guesser")
	if a.kexBackoff.level != 4 {
		t.Fatalf("back after two quiet hours: level %d, want 4 (two forgiven, one more)", a.kexBackoff.level)
	}

	b := &Agent{}
	for i := 0; i < kexLongBurst; i++ {
		b.allowKexFrom(now, "")
		b.markProvenSource("") // each one opened a frame: a person, not a guess
		now = now.Add(kexLongRefill)
	}
	if b.kexBackoff.free < kexLongBurst-1 {
		t.Fatalf("proven handshakes spent the allowance: %v left", b.kexBackoff.free)
	}
}

// The relay names each handshake's source, so a relay guessing the PIN could
// name a source that proved it. That source keeps being answered during the
// backoff only while its handshakes go on proving the PIN.
func TestKexBackoffCannotBeSkippedByBorrowingAProvenSource(t *testing.T) {
	a := &Agent{}
	a.markProvenSource("phone")
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < 200; i++ {
		a.allowKexFrom(now, "guesser")
		now = now.Add(kexLongRefill)
	}
	// The real phone: each handshake opens a frame, so it is never held.
	for i := 0; i < 20; i++ {
		if ok, _ := a.allowKexFrom(now, "phone"); !ok {
			t.Fatalf("the real phone was held at its connect %d", i)
		}
		a.markProvenSource("phone")
		a.allowKexFrom(now, "guesser") // still at it
		now = now.Add(kexLongRefill)
	}
	if !now.Before(a.kexBackoff.until) {
		t.Fatal("the backoff is not holding unproven sources")
	}
	// Guesses tagged as the phone, as fast as they come: answered kexOwedMax
	// times, then held like any other unproven source.
	borrowed := 0
	for i := 0; i < 50; i++ {
		if ok, _ := a.allowKexFrom(now, "phone"); ok {
			borrowed++
		}
		now = now.Add(time.Second)
	}
	if borrowed != kexOwedMax {
		t.Fatalf("guesses under a proven source's name: %d answered, want %d", borrowed, kexOwedMax)
	}
}
