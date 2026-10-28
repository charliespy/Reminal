// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"testing"
	"time"
)

// A session's sleep marks live beside its record and outlast its saves: a
// save rewrites the record whole and must not wake it.
func TestSleepMarksOutlastSaves(t *testing.T) {
	const id = "SLEEPA01"
	if err := WriteRestore(Restore{ID: id, PIN: "12345678", Fg: "claude", SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Truncate(time.Second)
	if err := MarkAsleep(id, Asleep{SleptAt: at, Why: "asked", Fg: "claude", Dropped: []string{"a job"}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := WriteRestore(Restore{ID: id, PIN: "12345678", Fg: "claude", SavedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	a := ReadAsleep(id)
	if a == nil || !a.SleptAt.Equal(at) || a.Why != "asked" || len(a.Dropped) != 1 {
		t.Fatalf("asleep mark after three saves: %+v", a)
	}
	if _, ok := AsleepIDs()[id]; !ok {
		t.Fatalf("not listed asleep: %v", AsleepIDs())
	}
	ClearAsleep(id)
	if IsAsleep(id) {
		t.Fatal("still asleep after it woke")
	}
}

// A wake under way is remembered (to be tried again after a restart), and
// forgotten when it did not finish: the session sleeps on.
func TestWakingMark(t *testing.T) {
	const id = "SLEEPA02"
	if _, ok := WakingSince(id); ok {
		t.Fatal("waking before any wake")
	}
	at := time.Now().Add(-time.Minute).UTC()
	if err := MarkWaking(id, at); err != nil {
		t.Fatal(err)
	}
	if got, ok := WakingSince(id); !ok || !got.Equal(at) {
		t.Fatalf("waking since %v %v, want %v", got, ok, at)
	}
	ClearWaking(id)
	if _, ok := WakingSince(id); ok {
		t.Fatal("waking mark kept after ClearWaking")
	}
}

// Keep awake is a mark of its own, on and off.
func TestAwakePin(t *testing.T) {
	const id = "SLEEPA03"
	if AwakePinned(id) {
		t.Fatal("pinned before asked")
	}
	if err := SetAwakePin(id, true); err != nil || !AwakePinned(id) {
		t.Fatalf("not pinned: %v", err)
	}
	if err := SetAwakePin(id, false); err != nil || AwakePinned(id) {
		t.Fatalf("still pinned: %v", err)
	}
}

// Ending a session for good takes its sleep marks with it: nothing is left
// asleep after a kill.
func TestClearRestoreTakesTheSleepMarks(t *testing.T) {
	const id = "SLEEPA04"
	_ = WriteRestore(Restore{ID: id, PIN: "12345678", SavedAt: time.Now()})
	_ = MarkAsleep(id, Asleep{SleptAt: time.Now(), Why: "asked"})
	_ = MarkWaking(id, time.Now())
	_ = SetAwakePin(id, true)
	if err := ClearRestore(id); err != nil {
		t.Fatal(err)
	}
	if IsAsleep(id) || AwakePinned(id) {
		t.Fatal("a mark outlived ClearRestore")
	}
	if _, ok := WakingSince(id); ok {
		t.Fatal("the waking mark outlived ClearRestore")
	}
}
