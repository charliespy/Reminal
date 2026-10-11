// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/vt"

	"reminal/internal/atomicfile"
	"reminal/internal/atrest"
)

// envResumeScrollback is the path of a 0600 dump written just before a hot
// restart. The session key is NOT carried across the exec (viewers re-EKE),
// so the dump is sealed under a one-time key handed to the successor in
// envResumeScrollbackKey — the key never touches disk — and the successor
// re-encrypts the history under its own session key.
const (
	envResumeScrollback    = "REMINAL_RESUME_SCROLLBACK"
	envResumeScrollbackKey = "REMINAL_RESUME_SCROLLBACK_KEY"
)

const kindHandoff = "handoff"

const scrollbackDumpVersion = 1

type scrollbackDump struct {
	Version  int               `json:"v"`
	NextSeq  uint64            `json:"next_seq"`
	BaseCols int               `json:"base_cols"`
	BaseRows int               `json:"base_rows"`
	Entries  []scrollDumpEntry `json:"entries"`
	// History is the snapshot emulator's scrollback as it was rendered (SGR
	// kept), at most scrollbackLines, and HistoryCols the width it was
	// rendered at. Entries are only the raw output ring (scrollbackBytes):
	// replayed alone, a successor's history is whatever the last 2 MiB of
	// output scrolled — on a busy agent's repaints, about a screen — so every
	// hot restart and reboot restore threw the rest away. A dump from an older
	// agent has none, and is replayed as before.
	History     []string `json:"history,omitempty"`
	HistoryCols int      `json:"history_cols,omitempty"`
}

type scrollDumpEntry struct {
	Seq  uint64 `json:"seq"`
	Data []byte `json:"data,omitempty"`
	Bar  bool   `json:"bar,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func scrollbackDumpPath(sessionID string) (string, error) {
	dir, err := reminalDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "scrollback-"+sessionID+".json"), nil
}

// writeScrollbackDump writes the live buffer, sealed under a fresh one-time
// key, to a 0600 file the successor will load, and returns the key as hex.
// Empty history yields ("", "", nil) — no file.
func (a *Agent) writeScrollbackDump() (string, string, error) {
	if a == nil || a.buf == nil || a.box == nil || a.sessionID == "" {
		return "", "", nil
	}
	path, err := scrollbackDumpPath(a.sessionID)
	if err != nil {
		return "", "", err
	}
	if len(a.buf.From(0)) == 0 {
		return "", "", nil
	}
	key, err := atrest.NewKey()
	if err != nil {
		return "", "", err
	}
	id := a.sessionID
	err = a.writeScrollbackDumpTo(path, func(b []byte) ([]byte, error) {
		return atrest.SealWith(key, kindHandoff, id, b)
	})
	return path, hex.EncodeToString(key), err
}

// writeScrollbackDumpTo writes the live buffer, decrypted from the session
// key and sealed by seal, to path (0600). An empty buffer writes nothing.
func (a *Agent) writeScrollbackDumpTo(path string, seal func([]byte) ([]byte, error)) error {
	if a == nil || a.buf == nil || a.box == nil {
		return nil
	}
	raw := a.buf.From(0)
	if len(raw) == 0 {
		return nil
	}
	dump := scrollbackDump{
		Version: scrollbackDumpVersion,
		NextSeq: a.buf.LatestSeq(),
	}
	dump.BaseCols, dump.BaseRows = a.buf.Base()
	if a.screen != nil {
		a.screenMu.Lock()
		// The screen's rows too: what an app left above a frame it repaints
		// in place never scrolls into scrollback, and its ring holds only the
		// repaints. Should the replay draw them again, the copy is the
		// history's tail matching the frame, which the snapshot drops
		// (dedupAgainstFrame).
		dump.History = renderScrollback(a.screen, a.scrollbackLines)
		if !a.screen.IsAltScreen() { // a full-screen app's screen is not history
			dump.History = append(dump.History, renderedScreenRows(a.screen)...)
		}
		if n := len(dump.History) - a.scrollbackLines; a.scrollbackLines > 0 && n > 0 {
			dump.History = dump.History[n:]
		}
		dump.HistoryCols = a.screen.Width()
		a.screenMu.Unlock()
	}
	dump.Entries = make([]scrollDumpEntry, 0, len(raw))
	for _, e := range raw {
		de := scrollDumpEntry{Seq: e.Seq, Bar: e.Bar, Cols: e.Cols, Rows: e.Rows}
		if e.Data != "" {
			pt, err := a.box.Decrypt(e.Data)
			if err != nil {
				return fmt.Errorf("decrypt scrollback seq %d: %w", e.Seq, err)
			}
			de.Data = pt
		}
		dump.Entries = append(dump.Entries, de)
	}
	body, err := json.Marshal(dump)
	if err != nil {
		return err
	}
	body, err = seal(body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Unique temp name, replacing rename, flushed: no window where the old
	// file is gone and the new one not there, and no two writers removing
	// each other's file.
	return atomicfile.Write(path, body, 0o600)
}

// dumpScrollbackForRestart writes the dump if there is history and returns
// the env entries that hand it to the successor. A write error must not
// block restart — we notify and continue without a file.
func (a *Agent) dumpScrollbackForRestart() (path string, env []string) {
	path, key, err := a.writeScrollbackDump()
	if err != nil {
		agentNotify("  reminal: could not save scrollback for restart — history will reset: %v\n", err)
		removeScrollbackDump(path)
		return "", nil
	}
	if path == "" {
		return "", nil
	}
	return path, []string{envResumeScrollback + "=" + path, envResumeScrollbackKey + "=" + key}
}

// removeScrollbackDump deletes a dump and its .tmp sibling. Safe on "" / missing.
func removeScrollbackDump(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
	_ = os.Remove(path + ".tmp")
}

// takeScrollbackDump loads and deletes a dump written by the predecessor,
// opened with the one-time key it handed over (hex). A predecessor from
// before sealing passes no key and a plain dump. Always deletes path and
// path+".tmp" so nothing lingers after a corrupt / wrong-version file.
// Missing or unreadable files return nil so restart still comes up (just
// without history).
func takeScrollbackDump(path, keyHex, sessionID string) *scrollbackDump {
	if path == "" {
		return nil
	}
	d := readScrollbackDump(path, func(b []byte) ([]byte, error) {
		if !atrest.IsSealed(b) {
			return b, nil // an older predecessor's plain dump
		}
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return nil, err
		}
		return atrest.OpenWith(key, kindHandoff, sessionID, b)
	})
	removeScrollbackDump(path)
	return d
}

// readScrollbackDump loads a dump, opened by open, and leaves it in place —
// a restore that fails can be tried again. Nil when missing, unreadable or
// empty.
func readScrollbackDump(path string, open func([]byte) ([]byte, error)) *scrollbackDump {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if body, err = open(body); err != nil {
		return nil
	}
	var dump scrollbackDump
	if json.Unmarshal(body, &dump) != nil || dump.Version != scrollbackDumpVersion {
		return nil
	}
	if len(dump.Entries) == 0 {
		return nil
	}
	return &dump
}

// restoreResumedScrollback re-encrypts a predecessor dump under this agent's
// new session key and replays it into the snapshot emulator. Must run after
// initScreen and before the PTY pump, or live output interleaves with history.
func (a *Agent) restoreResumedScrollback() {
	d := a.resumeDump
	a.resumeDump = nil
	if d == nil || a.buf == nil || a.box == nil {
		return
	}
	entries := make([]scrollEntry, 0, len(d.Entries))
	for _, e := range d.Entries {
		se := scrollEntry{Seq: e.Seq, Bar: e.Bar, Cols: e.Cols, Rows: e.Rows}
		if len(e.Data) > 0 {
			enc, err := a.box.Encrypt(e.Data)
			if err != nil {
				agentNotify("  reminal: could not restore scrollback: %v\n", err)
				return
			}
			se.Data = enc
		}
		entries = append(entries, se)
	}
	a.buf.restore(entries, d.NextSeq, d.BaseCols, d.BaseRows)

	a.screenMu.Lock()
	defer a.screenMu.Unlock()
	if a.screen == nil {
		return
	}
	if len(d.History) > 0 {
		// Only what the ring will not draw again: replayed into a scratch
		// emulator first (once, at a restart), what it reproduces is matched
		// against the history's tail, and the history goes in up to there.
		scratch := vt.NewEmulator(max(d.BaseCols, 1), max(d.BaseRows, 1))
		scratch.Scrollback().SetMaxLines(len(d.History) + 1000)
		go io.Copy(io.Discard, scratch)
		replayDump(scratch, d)
		again := append(renderScrollback(scratch, 0), renderedScreenRows(scratch)...)
		// Not closed: Close races the drain's Read (vt). Its memory goes
		// instead; its drain idles out the process, one per restart.
		_, _ = scratch.Write([]byte("\x1bc"))
		scratch.ClearScrollback()
		scratch.Resize(1, 1)
		restoreHistory(a.screen, d.History[:historySeam(d.History, again)], d.HistoryCols)
		a.noteScrollbackLen()
	}
	replayDump(a.screen, d, a.noteScrollbackLen)
}

// restoreHistory puts a predecessor's rendered scrollback (scrollbackDump.
// History) into e's scrollback, whole, before its output ring is replayed:
// each line at the width it was rendered at, then a screen of blank lines to
// push the last of them up, then the screen cleared without pushing (ED 0
// from home) for the replay to draw on. What the ring itself scrolls again is
// a copy of the history's tail; the snapshot's dedup drops it (dedupBlocks).
func restoreHistory(e *vt.Emulator, history []string, cols int) {
	if e == nil || len(history) == 0 {
		return
	}
	if cols > 0 && cols != e.Width() {
		resizeAnchoredBottom(e, cols, e.Height())
	}
	var b strings.Builder
	for _, l := range history {
		b.WriteString(l)
		b.WriteString("\x1b[m\r\n")
	}
	b.WriteString(strings.Repeat("\r\n", e.Height()))
	b.WriteString("\x1b[H\x1b[J")
	_, _ = e.Write([]byte(b.String()))
}

// renderedScreenRows is e's screen rendered, a line a row, without the blank rows
// below its last text.
func renderedScreenRows(e *vt.Emulator) []string {
	rows := strings.Split(e.Render(), "\n")
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	for len(rows) > 0 && stripANSI(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// replayDump replays a dump's output ring into e, as it was recorded: its
// sizes and its output, in order. after, if given, runs after each step.
func replayDump(e *vt.Emulator, d *scrollbackDump, after ...func()) {
	step := func() {
		for _, f := range after {
			f()
		}
	}
	if d.BaseCols > 0 && d.BaseRows > 0 {
		resizeAnchoredBottom(e, d.BaseCols, d.BaseRows)
		step()
	}
	for _, en := range d.Entries {
		if en.Cols > 0 && en.Rows > 0 {
			resizeAnchoredBottom(e, en.Cols, en.Rows)
			step()
			continue
		}
		if len(en.Data) > 0 {
			_, _ = e.Write(en.Data)
			step()
		}
	}
}

// historySeam is how much of history to put back before the ring is
// replayed: all of it but the tail the ring draws again (again: what the
// replay alone renders). The tail is found as the longest run of lines the
// two share, starting from one of again's first non-blank lines (a ring cut
// mid-frame begins with debris) and running to the history's end; with none
// of three lines or more, the ring draws nothing of it and all of it goes in.
func historySeam(history, again []string) int {
	plain := func(ls []string) []string {
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = strings.TrimRight(stripANSI(l), " ")
		}
		return out
	}
	h, r := plain(history), plain(again)
	at := make(map[string][]int, len(h))
	for i, l := range h {
		if l != "" {
			at[l] = append(at[l], i)
		}
	}
	best, bestRun := len(h), 0
	for j := 0; j < len(r) && j < 64; j++ {
		if r[j] == "" {
			continue
		}
		for _, p := range at[r[j]] {
			run := 0
			for p+run < len(h) && j+run < len(r) && h[p+run] == r[j+run] {
				run++
			}
			if run > bestRun && (p+run == len(h) || j+run == len(r)) {
				best, bestRun = p-j, run
			}
		}
	}
	if bestRun < 3 {
		return len(h)
	}
	return max(best, 0)
}
