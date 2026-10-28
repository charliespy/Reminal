// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"reminal/internal/protocol"
	"reminal/internal/session"
)

// Sleep: a session that ends its processes the way a machine shutdown does,
// its restore record kept, and comes back on the same id, PIN and
// conversation when it is woken (RestoreSessionWith, the path a reboot
// takes). Its memory is freed meanwhile. It sleeps only when it can come
// back to exactly where it was, and never while something is going on in
// it: `reminal sleep` says why not.

// sleepFacts is what decides whether a session may sleep now.
type sleepFacts struct {
	Pinned   bool     // the person asked it to stay awake
	Headless bool     // started by reminal (not a terminal tab, which is the session)
	Attn     string   // its agent's attention state
	Shells   int      // background shells its agent said it left running
	Agents   int      // background subagents
	Jobs     []string // other programs running in its shell
	Attached int      // `reminal connect` terminals attached to it
	Fg       string   // its coding agent, as the saved record names it
	Conv     string   // the agent's conversation, as saved
	NoTurn   bool     // the agent was never sent a message (nothing to resume)
	Saved    bool     // its record was just saved
}

// sleepVerdict says whether a session may sleep now, in the words its card
// and `reminal sleep` use. block is a reason it can't, whatever the person
// says: it would not come back as itself. losses are what a sleep would end
// that a wake can't bring back: a job running in it, an unfinished turn. It
// sleeps with none, or with them when the person says "sleep anyway", and
// its row then says what that let go of. Never lost silently.
func sleepVerdict(f sleepFacts) (block string, losses []string) {
	switch {
	case f.Pinned:
		return "kept awake · you asked (reminal keep-awake --off)", nil
	case !f.Headless:
		// The terminal tab is the session: closing it is ending it.
		return "kept awake · open in a terminal", nil
	case !f.Saved:
		return "kept awake · couldn't save", nil
	case f.Fg != "" && !sleepsExactly(f.Fg):
		return "kept awake · " + f.Fg + " can't be resumed exactly yet", nil
	case f.Fg != "" && !f.NoTurn && !session.ValidConv(f.Conv):
		return "kept awake · can't tell which conversation is its own", nil
	}
	switch f.Attn {
	case "working":
		losses = append(losses, "the agent's unfinished turn")
	case "input":
		losses = append(losses, "the question it was waiting on you for")
	}
	if f.Shells > 0 || f.Agents > 0 {
		losses = append(losses, "its agent's background work")
	}
	for _, j := range f.Jobs {
		losses = append(losses, j+", running in it")
	}
	if f.Attached > 0 {
		losses = append(losses, "the terminal attached to it")
	}
	if f.Fg == "" {
		// A shell's typed-by-hand state (variables, a cd, an alias) is not in
		// its record; it comes back a new shell in its folder.
		losses = append(losses, "what was typed into its shell by hand (variables, aliases)")
	}
	return "", losses
}

// sleepsExactly says agent fg comes back on exactly its own conversation:
// resumed by the id its hook reported, the conversation's file checked at
// wake. Others ("latest in this folder") wait for a check that no other run
// in the folder changed what "latest" is.
func sleepsExactly(fg string) bool { return fg == "claude" }

// errWouldLose is a sleep refused for what it would end; "sleep anyway"
// (anyway) lets those go.
type errWouldLose struct{ losses []string }

func (e errWouldLose) Error() string {
	return "awake · sleeping would end " + strings.Join(e.losses, "; ")
}

// sleepNow puts this session to sleep, or says why not (control "sleep",
// "sleep anyway"). It saves its record first and checks the save: the record
// is what it wakes from.
func (a *Agent) sleepNow(anyway bool) error {
	a.metaMu.Lock()
	f := sleepFacts{
		Pinned: session.AwakePinned(a.sessionID), Headless: a.headless,
		Attn: a.attnState, Shells: a.attnShells, Agents: a.attnAgents,
	}
	a.metaMu.Unlock()
	a.currentConnMu.Lock()
	f.Attached = len(a.localConns)
	a.currentConnMu.Unlock()
	if a.term != nil {
		f.Jobs = otherJobs(a.term)
	}
	before := time.Now()
	a.saveRestore()
	r, err := session.ReadRestore(a.sessionID)
	f.Saved = err == nil && r != nil && !r.SavedAt.Before(before)
	if f.Saved {
		f.Fg, f.Conv, f.NoTurn = r.Fg, r.Conv, r.NoTurn
	}
	block, losses := sleepVerdict(f)
	if block != "" {
		return errors.New(block)
	}
	if len(losses) > 0 && !anyway {
		return errWouldLose{losses}
	}
	if err := session.MarkAsleep(a.sessionID, session.Asleep{SleptAt: time.Now(), Why: "asked", Fg: r.Fg, Dropped: losses}); err != nil {
		return fmt.Errorf("kept awake · couldn't save: %w", err)
	}
	return nil
}

// wakeCheck is why sleeping session r can't wake exactly, or "": its folder
// is there, and the conversation it slept in still is.
func wakeCheck(r session.Restore) string {
	if r.Cwd != "" {
		if fi, err := os.Stat(r.Cwd); err != nil || !fi.IsDir() {
			return "can't wake · its folder is gone (" + r.Cwd + ")"
		}
	}
	if r.Fg == "claude" && !r.NoTurn && claudeConvSize(r.Conv) <= 0 {
		return "can't wake · claude's conversation " + r.Conv + " is gone"
	}
	return ""
}

// WakeSession wakes sleeping session r: back on its id, PIN and
// conversation, the way a reboot brings it back. A wake that does not come
// up leaves it asleep, to be tried again.
func WakeSession(r session.Restore) (*SpawnedSession, error) {
	if why := wakeCheck(r); why != "" {
		return nil, errors.New(why)
	}
	_ = session.MarkWaking(r.ID, time.Now())
	sp, err := RestoreSessionWith(r, "woke from sleep")
	if err != nil && !SessionRunning(r.ID) {
		session.ClearWaking(r.ID)
		return nil, err
	}
	session.ClearAsleep(r.ID)
	return sp, nil
}

// sleeperIndex keeps what a directory listing shows of each sleeping
// session (its name, folder, agent), read from its sealed record once and
// again only when its asleep mark changes: the directory is polled often,
// and opening hundreds of sealed records on every poll is not free.
var sleeperIndex = struct {
	sync.Mutex
	m map[string]sleeperRow
}{m: map[string]sleeperRow{}}

type sleeperRow struct {
	marked time.Time // the asleep mark's time when this was read
	ds     protocol.DirSession
}

// sleeperDirSessions are the directory entries of the sessions asleep.
func sleeperDirSessions() []protocol.DirSession {
	marks := session.AsleepIDs()
	sleeperIndex.Lock()
	defer sleeperIndex.Unlock()
	for id := range sleeperIndex.m {
		if _, ok := marks[id]; !ok {
			delete(sleeperIndex.m, id) // woke, or was ended
		}
	}
	var out []protocol.DirSession
	for id, at := range marks {
		row, ok := sleeperIndex.m[id]
		if !ok || !row.marked.Equal(at) {
			r, err := session.ReadRestore(id)
			a := session.ReadAsleep(id)
			if err != nil || r == nil || a == nil {
				continue // locked, or half written: the next poll tries again
			}
			row = sleeperRow{marked: at, ds: protocol.DirSession{
				ID: r.ID, Name: r.Name, Cwd: r.Cwd, Headless: true, Fg: r.Fg,
				Asleep: true, SleptAt: a.SleptAt.Unix(), Dropped: a.Dropped,
			}}
			sleeperIndex.m[id] = row
		}
		if SessionRunning(id) {
			continue // waking: it lists as itself
		}
		out = append(out, row.ds)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SleptAt > out[j].SleptAt })
	return out
}

// sleeperTranscript is a sleeping session's scrollback as text, from what
// it saved as it went to sleep: read_transcript and search_sessions answer
// from it without waking the session. ok is false when it saved none.
func sleeperTranscript(id string) (text string, truncated, ok bool) {
	p, err := session.RestoreScrollbackPath(id)
	if err != nil {
		return "", false, false
	}
	dump := readScrollbackDump(p, func(b []byte) ([]byte, error) { return session.OpenScrollback(id, b) })
	if dump == nil {
		return "", false, false
	}
	var b strings.Builder
	for _, h := range dump.History {
		b.WriteString(h)
		b.WriteByte('\n')
	}
	for _, e := range dump.Entries {
		b.Write(e.Data)
	}
	text, truncated = clipTranscript(stripANSI(b.String()), maxTranscriptBytes)
	return text, truncated, true
}

// SearchSleeperTranscript is search_sessions' hits in a sleeping session's
// saved scrollback (nil for none, or a session not asleep).
func SearchSleeperTranscript(id, pattern string) []string {
	if !session.IsAsleep(id) {
		return nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	text, _, ok := sleeperTranscript(id)
	if !ok {
		return nil
	}
	return transcriptHits(text, re)
}

// wakeRetryWithin: a wake the daemon was stopped in the middle of is tried
// again at its start if it began this recently; an older one sleeps on.
const wakeRetryWithin = 10 * time.Minute

// sleepsOn says restore record r is a session asleep, not one a restart
// ended: left alone at start, unless its wake was cut short just now.
func sleepsOn(id string, now time.Time) bool {
	if !session.IsAsleep(id) {
		return false
	}
	at, ok := session.WakingSince(id)
	return !ok || now.Sub(at) > wakeRetryWithin
}
