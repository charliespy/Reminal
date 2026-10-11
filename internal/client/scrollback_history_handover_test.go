// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"

	"reminal/internal/crypto"
)

// inkLikeStream is a busy coding agent's output as a terminal gets it: 3000
// shell lines, then an answer of 2000 lines streamed above a live frame of 8
// rows that is erased and rewritten (Ink's log-update), then the frame alone,
// repainting (the next turn thinking) until well past the output ring.
func inkLikeStream() []byte {
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	frameH, tick := 0, 0
	frame := func() {
		for i := 0; i < frameH; i++ {
			b.WriteString("\x1b[2K")
			if i < frameH-1 {
				b.WriteString("\x1b[1A")
			}
		}
		b.WriteString("\r")
		fmt.Fprintf(&b, "\x1b[38;2;215;119;87m* Thinking… (%d.%ds · esc to interrupt)\x1b[39m", tick/20, tick%20/2)
		for i := 0; i < 6; i++ {
			fmt.Fprintf(&b, "\r\n\x1b[2m| live row %d · · · · · · · · · · · · · · · · · · · · · · ·\x1b[22m", i)
		}
		b.WriteString("\r\n\x1b[7m> \x1b[27m")
		frameH = 8
		tick++
	}
	for n := 20001; n <= 22000; n++ {
		if frameH > 0 {
			for i := 0; i < frameH; i++ {
				b.WriteString("\x1b[2K")
				if i < frameH-1 {
					b.WriteString("\x1b[1A")
				}
			}
			b.WriteString("\r")
			frameH = 0
		}
		fmt.Fprintf(&b, "line %d of the answer, committed above the live frame\r\n", n)
		if n%5 == 0 {
			frame()
		}
	}
	for b.Len() < 3*scrollbackBytes {
		frame()
	}
	return []byte(b.String())
}

// distinctLines counts the shell (1..3000) and answer (20001..22000) lines
// text holds.
func distinctLines(text string) (shell, answer int) {
	seen := map[int]bool{}
	for _, m := range regexp.MustCompile(`line (\d+)\b`).FindAllStringSubmatch(text, -1) {
		n, _ := strconv.Atoi(m[1])
		seen[n] = true
	}
	for n := range seen {
		switch {
		case n >= 1 && n <= 3000:
			shell++
		case n >= 20001 && n <= 22000:
			answer++
		}
	}
	return
}

func handoverAgent(t *testing.T, box *crypto.Box) *Agent {
	t.Helper()
	a := &Agent{box: box, buf: newScrollback(scrollbackBytes), scrollbackLines: 10000, sessionID: "HANDOVR1"}
	a.screen = vt.NewEmulator(120, 30)
	a.screen.Scrollback().SetMaxLines(a.scrollbackLines)
	go io.Copy(io.Discard, a.screen)
	a.buf.SetBase(120, 30)
	return a
}

// A hot restart (and a reboot restore, which reads the same dump) hands the
// successor the history a viewer could scroll through, not only what the last
// scrollbackBytes of raw output scrolled. A busy agent's repaints fill that
// ring within minutes: replayed alone it held about a screen, the "scrollback
// is 1.5× the screen" of a session hot-restarted by its upgrades.
func TestAHotRestartKeepsTheWholeHistory(t *testing.T) {
	key, _ := crypto.NewSessionKey()
	box, _ := crypto.NewBox(key)
	a := handoverAgent(t, box)
	raw := inkLikeStream()
	if len(raw) <= scrollbackBytes {
		t.Fatalf("fixture: %d bytes does not overflow the %d-byte ring", len(raw), scrollbackBytes)
	}
	for i := 0; i < len(raw); i += 4096 {
		a.record(raw[i:min(i+4096, len(raw))])
	}
	a.screenMu.Lock()
	before := stripANSI(buildSnapshot(a.screen, a.snapshotHistory(), nil, maxSnapshotPlaintext, false))
	a.screenMu.Unlock()
	if s, an := distinctLines(before); s != 3000 || an != 2000 {
		t.Fatalf("fixture: before the restart a viewer sees %d/3000 shell and %d/2000 answer lines", s, an)
	}

	path, k, err := a.writeScrollbackDump()
	if err != nil || path == "" {
		t.Fatalf("dump: %q %v", path, err)
	}
	b := handoverAgent(t, box)
	b.resumeDump = takeScrollbackDump(path, k, a.sessionID)
	if b.resumeDump == nil {
		t.Fatal("the dump did not come back")
	}
	b.restoreResumedScrollback()
	b.screenMu.Lock()
	after := stripANSI(buildSnapshot(b.screen, b.snapshotHistory(), nil, maxSnapshotPlaintext, false))
	b.screenMu.Unlock()
	s, an := distinctLines(after)
	if s != 3000 || an != 2000 {
		var miss []int
		for n := 20001; n <= 22000; n++ {
			if !strings.Contains(after, fmt.Sprintf("line %d ", n)) {
				miss = append(miss, n)
			}
		}
		t.Fatalf("after a hot restart a viewer sees %d/3000 shell and %d/2000 answer lines; missing %v", s, an, miss)
	}
	// What the ring scrolls again is the history's own tail: it must not
	// show twice.
	for _, n := range []int{1, 1500, 3000, 20001, 21000, 22000} {
		if c := strings.Count(after, fmt.Sprintf("line %d ", n)) + strings.Count(after, fmt.Sprintf("line %d\n", n)); c != 1 {
			t.Errorf("line %d shows %d times after the restart", n, c)
		}
	}
}

// A dump from before History (an older agent's) is replayed as it always was.
func TestADumpWithoutHistoryIsReplayedAsBefore(t *testing.T) {
	key, _ := crypto.NewSessionKey()
	box, _ := crypto.NewBox(key)
	a := handoverAgent(t, box)
	for i := 1; i <= 50; i++ {
		a.record([]byte(fmt.Sprintf("line %d\r\n", i)))
	}
	path, k, err := a.writeScrollbackDump()
	if err != nil {
		t.Fatal(err)
	}
	b := handoverAgent(t, box)
	b.resumeDump = takeScrollbackDump(path, k, a.sessionID)
	b.resumeDump.History, b.resumeDump.HistoryCols = nil, 0
	b.restoreResumedScrollback()
	b.screenMu.Lock()
	after := stripANSI(buildSnapshot(b.screen, b.snapshotHistory(), nil, maxSnapshotPlaintext, false))
	b.screenMu.Unlock()
	if s, _ := distinctLines(after); s != 50 {
		t.Fatalf("an old dump's ring replay gave %d/50 lines", s)
	}
}

// When the ring still holds all of the history (output that cost few bytes:
// a pseudo console sends only the cells a repaint changed), its replay draws
// every line again: put back from the dump as well, each would show twice.
func TestAHotRestartWithTheHistoryInItsRingShowsNothingTwice(t *testing.T) {
	key, _ := crypto.NewSessionKey()
	box, _ := crypto.NewBox(key)
	a := handoverAgent(t, box)
	var b strings.Builder
	for i := 1; i <= 3000; i++ {
		fmt.Fprintf(&b, "line %d\r\n", i)
	}
	for n := 20001; n <= 22000; n++ {
		fmt.Fprintf(&b, "line %d of the answer, committed above the live frame\r\n", n)
	}
	b.WriteString("\x1b[7m> \x1b[27m")
	a.record([]byte(b.String()))
	if len(a.buf.From(0)) == 0 || b.Len() > scrollbackBytes {
		t.Fatal("fixture: the whole history should fit the ring")
	}
	path, k, err := a.writeScrollbackDump()
	if err != nil || path == "" {
		t.Fatalf("dump: %q %v", path, err)
	}
	r := handoverAgent(t, box)
	r.resumeDump = takeScrollbackDump(path, k, a.sessionID)
	r.restoreResumedScrollback()
	r.screenMu.Lock()
	after := stripANSI(buildSnapshot(r.screen, r.snapshotHistory(), nil, maxSnapshotPlaintext, false))
	sb := r.screen.Scrollback().Len()
	r.screenMu.Unlock()
	if s, an := distinctLines(after); s != 3000 || an != 2000 {
		t.Fatalf("after a hot restart a viewer sees %d/3000 shell and %d/2000 answer lines", s, an)
	}
	twice := 0
	for n := 1; n <= 3000; n++ {
		if strings.Count(after, fmt.Sprintf("line %d\n", n)) > 1 {
			twice++
		}
	}
	for n := 20001; n <= 22000; n++ {
		if strings.Count(after, fmt.Sprintf("line %d ", n)) > 1 {
			twice++
		}
	}
	if twice > 0 || sb > 5000 {
		t.Fatalf("%d lines show twice after the restart (scrollback %d lines for 5000)", twice, sb)
	}
}
