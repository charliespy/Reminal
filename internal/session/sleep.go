// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
)

// A session asleep has ended its processes the way a machine shutdown does,
// its restore record kept, to come back on the same id, PIN and conversation
// when it is woken. What says so lives beside the record, in files of their
// own: the record is rebuilt whole on every save, and would lose a mark kept
// in it. None of them holds a secret (the conversation id stays in the
// sealed record); each is written 0600 all the same.
//
//	<ID>.asleep     it sleeps: when, and why
//	<ID>.waking     a wake under way, since when (a wake the daemon was
//	                stopped in the middle of is tried again)
//	<ID>.awake-pin  the person asked it to stay awake
const (
	sufAsleep   = ".asleep"
	sufWaking   = ".waking"
	sufAwakePin = ".awake-pin"
)

// Asleep is what <ID>.asleep says.
type Asleep struct {
	SleptAt time.Time `json:"slept_at"`
	// Why it sleeps, in a word: "asked" (`reminal sleep`, or Sleep now).
	Why string `json:"why"`
	// Fg is the coding agent it will wake with, by name ("" for a shell).
	Fg string `json:"fg,omitempty"`
	// Dropped is what the person chose to let go of by sleeping anyway (a
	// job that was running in it, an unfinished turn), shown on its row.
	Dropped []string `json:"dropped,omitempty"`
}

// MarkAsleep records that session id sleeps.
func MarkAsleep(id string, a Asleep) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return writeSidecar(id, sufAsleep, b)
}

// ReadAsleep is session id's asleep mark, or nil when it is not asleep.
func ReadAsleep(id string) *Asleep {
	b, err := readSidecar(id, sufAsleep)
	if err != nil {
		return nil
	}
	var a Asleep
	if json.Unmarshal(b, &a) != nil || a.SleptAt.IsZero() {
		return nil
	}
	return &a
}

// IsAsleep says session id sleeps.
func IsAsleep(id string) bool { return ReadAsleep(id) != nil }

// MarkWaking records that a wake of session id began at.
func MarkWaking(id string, at time.Time) error {
	return writeSidecar(id, sufWaking, []byte(at.UTC().Format(time.RFC3339Nano)))
}

// WakingSince is when a wake of session id began, if one is under way.
func WakingSince(id string) (time.Time, bool) {
	b, err := readSidecar(id, sufWaking)
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	return t, err == nil
}

// ClearWaking forgets a wake of session id that did not finish: it sleeps on.
func ClearWaking(id string) { removeSidecar(id, sufWaking) }

// ClearAsleep marks session id awake again: it woke.
func ClearAsleep(id string) {
	removeSidecar(id, sufAsleep)
	removeSidecar(id, sufWaking)
}

// SetAwakePin keeps session id awake (on), or lets it sleep again.
func SetAwakePin(id string, on bool) error {
	if !on {
		removeSidecar(id, sufAwakePin)
		return nil
	}
	return writeSidecar(id, sufAwakePin, []byte(time.Now().UTC().Format(time.RFC3339)))
}

// AwakePinned says the person asked session id to stay awake.
func AwakePinned(id string) bool {
	_, err := readSidecar(id, sufAwakePin)
	return err == nil
}

func writeSidecar(id, suf string, b []byte) error {
	p, err := restorePath(strings.ToUpper(id), suf)
	if err != nil {
		return err
	}
	return writeFileAtomic(p, b)
}

func readSidecar(id, suf string) ([]byte, error) {
	p, err := restorePath(strings.ToUpper(id), suf)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err == nil && len(b) == 0 {
		return nil, errors.New("empty")
	}
	return b, err
}

func removeSidecar(id, suf string) {
	if p, err := restorePath(strings.ToUpper(id), suf); err == nil {
		_ = os.Remove(p)
	}
}
