// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reminal/internal/atrest"
	"time"
)

// HookState is a per-session attention state reported by an integrated coding
// agent's own lifecycle hook (see `reminal integrate` / `reminal hook`). It's the
// PRECISE signal: the agent tells us "working / input / done" directly, instead
// of us inferring it from the screen. Written to ~/.reminal/hook-<id>.state by the
// `reminal hook` command the harness fires; read by the attention detector, which
// prefers it over the screen-scrape fallback while it's fresh (HookStateTTL).
type HookState struct {
	State string    `json:"state"` // "working" | "input" | "done"
	TS    time.Time `json:"ts"`    // when the hook fired
	// Background is the work the harness said it still had running when its
	// turn ended: nil when it said nothing about it (a harness, or a build,
	// that does not), which is not the same as none.
	Background *Background `json:"background,omitempty"`
}

// Background is how many shells and subagents a harness still has running in
// the background, as its own hooks said: a turn that ended with them going is
// done, and its session is still at work.
type Background struct {
	Shells int `json:"shells,omitempty"`
	Agents int `json:"agents,omitempty"`
}

// HookStateTTL bounds how long a hook state is trusted without a fresh event. A
// harness fires on transitions (prompt→working, awaiting→input, stop→done), so a
// state legitimately stands between events — including a long "working" think.
// Past this, we assume the harness went away (crash, kill) and fall back to the
// screen detector. Generous on purpose: a false stale is just a graceful fallback.
const HookStateTTL = 15 * time.Minute

func hookStatePath(id string) (string, error) {
	if id == "" {
		return "", errors.New("hook state requires a session id")
	}
	dir, err := activeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hook-"+id+".state"), nil
}

// WriteHookState records the agent-reported attention state for a session.
func WriteHookState(id, state string) error {
	return WriteHookStateWith(id, state, nil)
}

// WriteHookStateWith records the agent-reported attention state for a
// session, with the background work it said it still has (nil: it said
// nothing about that).
func WriteHookStateWith(id, state string, bg *Background) error {
	return writeHookState(id, HookState{State: state, TS: time.Now(), Background: bg})
}

// UpdateHookBackground records the background work a harness still has
// running, said between turns (a subagent of its own finishing), leaving its
// turn's state and when that was said as they are: the state is the turn's,
// and a subagent finishing inside a turn does not end it. With no state yet
// there is no turn to attach it to, and nothing is written.
func UpdateHookBackground(id string, bg *Background) error {
	hs := ReadHookStateAnyAge(id)
	if hs == nil {
		return nil
	}
	hs.Background = bg
	return writeHookState(id, *hs)
}

func writeHookState(id string, hs HookState) error {
	p, err := hookStatePath(id)
	if err != nil {
		return err
	}
	if err := atrest.CheckWritable(); err != nil {
		return err // a test binary with the real HOME writes nothing here
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(hs)
	if err != nil {
		return err
	}
	// Written whole, or not at all. Two hooks can fire close enough together to
	// overlap — an agent reporting a state change as it exits, say — and writing
	// in place leaves a reader the tail of the longer record stuck onto the
	// shorter one. That does not parse, so the session silently loses its precise
	// state and falls back to reading the screen until the record expires.
	tmp, err := os.CreateTemp(filepath.Dir(p), "hook-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op once the rename lands
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// ReadHookState returns the fresh hook state for a session, or nil when there is
// none, it's unreadable, or it's older than HookStateTTL (stale → let the caller
// fall back to the screen detector).
func ReadHookState(id string) *HookState {
	hs := ReadHookStateAnyAge(id)
	if hs == nil || time.Since(hs.TS) > HookStateTTL {
		return nil
	}
	return hs
}

// ReadHookStateAnyAge is the session's hook state however old it is, for a
// caller that knows whether the program that wrote it is still running —
// a harness at rest stays at rest, and says nothing more, for as long as it
// is left there.
func ReadHookStateAnyAge(id string) *HookState {
	p, err := hookStatePath(id)
	if err != nil {
		return nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var hs HookState
	if err := json.Unmarshal(raw, &hs); err != nil {
		return nil
	}
	if hs.State == "" {
		return nil
	}
	return &hs
}

// ClearHookState removes a session's hook state (on session exit). Idempotent.
func ClearHookState(id string) error {
	p, err := hookStatePath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// AppendHookDebug adds one line to ~/.reminal/hook-debug.log: what a hook
// made of its event, for whoever set REMINAL_HOOK_DEBUG to check it. Only
// what the hook concluded goes in, never the payload it was given.
func AppendHookDebug(line string) error {
	dir, err := activeDir()
	if err != nil {
		return err
	}
	if err := atrest.CheckWritable(); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "hook-debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(time.Now().Format(time.RFC3339) + " " + line + "\n")
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
