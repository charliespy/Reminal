// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"

	"reminal/internal/config"
	"reminal/internal/crypto"
)

// colouredSession is an agent whose history is n syntax-coloured lines of
// about 600 bytes each (a coding agent's long answer), at 200 columns.
func colouredSession(t *testing.T, n, budget int) *Agent {
	return colouredSessionPal(t, n, budget, false)
}

// colouredSessionPal: random 24-bit colours instead of a palette (the history
// that compresses worst).
func colouredSessionPal(t *testing.T, n, budget int, random bool) *Agent {
	t.Helper()
	key, _ := crypto.NewSessionKey()
	box, _ := crypto.NewBox(key)
	a := &Agent{box: box, buf: newScrollback(scrollbackBytes), scrollbackLines: 10000, scrollbackBytes: budget, sessionID: "SNAPPART"}
	a.screen = vt.NewEmulator(200, 40)
	a.screen.Scrollback().SetMaxLines(10000)
	go io.Copy(io.Discard, a.screen)
	a.buf.SetBase(200, 40)
	words := strings.Fields("the agent read the file and found that the function returns early when the buffer is empty so the caller never sees the error")
	pal := [][3]int{{215, 119, 87}, {86, 156, 214}, {78, 201, 176}, {220, 220, 170}, {197, 134, 192}, {156, 220, 254}, {206, 145, 120}, {181, 206, 168}}
	rng := rand.New(rand.NewSource(1))
	for i := 1; i <= n; i++ {
		var b strings.Builder
		fmt.Fprintf(&b, "line %d ", i)
		for vis := 0; vis < 150; {
			w, c := words[rng.Intn(len(words))], pal[rng.Intn(len(pal))]
			if random {
				c = [3]int{rng.Intn(256), rng.Intn(256), rng.Intn(256)}
			}
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%s\x1b[39m ", c[0], c[1], c[2], w)
			vis += len(w) + 1
		}
		a.record([]byte(b.String() + "\r\n"))
	}
	return a
}

// unpackParts decrypts and joins a snapshot's parts as a viewer does, and
// checks each one's size and header on the way.
func unpackParts(t *testing.T, a *Agent, parts []string) string {
	t.Helper()
	var gz bytes.Buffer
	id := ""
	for i, p := range parts {
		if len(p) > 640<<10 {
			t.Fatalf("part %d is %d bytes encrypted; a part must stay under 640 KiB", i, len(p))
		}
		pt, err := a.box.Decrypt(p)
		if err != nil {
			t.Fatal(err)
		}
		hl := binary.BigEndian.Uint32(pt)
		var h struct {
			ID   string `json:"id"`
			I, N int
			To   string `json:"to"`
		}
		if err := json.Unmarshal(pt[4:4+hl], &h); err != nil || h.I != i || h.N != len(parts) || (id != "" && h.ID != id) || h.To != "v-test" {
			t.Fatalf("part %d header %+v (%v)", i, h, err)
		}
		id = h.ID
		gz.Write(pt[4+hl:])
	}
	r, err := gzip.NewReader(&gz)
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// partsFor is the parts a joiner "v-test" is sent, with the seq they cover.
func partsFor(t *testing.T, a *Agent) ([]string, uint64, error) {
	t.Helper()
	by, latest, err := a.snapshotParts([]string{"v-test"})
	return by["v-test"], latest, err
}

func numberedLines(s string) int {
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`line (\d+) `).FindAllStringSubmatch(stripANSI(s), -1) {
		seen[m[1]] = true
	}
	return len(seen)
}

// A joining viewer that can take parts gets the history the agent keeps for
// snapshots, not what one relay message holds: 4000 coloured lines (~2.4 MB),
// of which a plain snapshot carries the newest few hundred.
func TestASnapshotInPartsCarriesTheWholeHistory(t *testing.T) {
	a := colouredSession(t, 4000, config.SnapshotScrollbackBytes())
	parts, latest, err := partsFor(t, a)
	if err != nil || len(parts) == 0 || latest == 0 {
		t.Fatalf("parts: %d, latest %d, %v", len(parts), latest, err)
	}
	whole := unpackParts(t, a, parts)
	if got := numberedLines(whole); got != 4000 {
		t.Fatalf("a snapshot in parts carries %d/4000 lines", got)
	}
	frame, _ := a.snapshotFrame()
	pt, _ := a.box.Decrypt(frame)
	plain := numberedLines(string(pt))
	t.Logf("in parts: 4000/4000 lines, %d parts, %d B unpacked; the plain frame: %d/4000 lines, %d B", len(parts), len(whole), plain, len(frame))
	if plain >= 4000 {
		t.Fatalf("fixture: the plain frame should be the one cut short (it carried %d)", plain)
	}
	// An older viewer's frame is as it was: one message, under the relay's
	// 1 MiB, whatever the (larger) history cap.
	if len(frame) >= 1<<20 {
		t.Fatalf("the plain frame is %d B: over the relay's 1 MiB", len(frame))
	}
}

// The configured history cap (REMINAL_SCROLLBACK_BYTES) holds for parts too.
func TestASnapshotInPartsKeepsTheConfiguredCap(t *testing.T) {
	a := colouredSession(t, 4000, 1<<20)
	parts, _, _ := partsFor(t, a)
	whole := unpackParts(t, a, parts)
	if got := numberedLines(whole); got >= 4000 || got < 1000 {
		t.Fatalf("with a 1 MiB cap the parts carry %d/4000 lines", got)
	}
	if hist := len(whole); hist > (1<<20)+(1<<18) {
		t.Fatalf("with a 1 MiB cap the snapshot unpacks to %d B", hist)
	}
}

// Who gets parts: only joiners that asked, and none at all when any joiner
// owed a snapshot did not ask — the plain frame, broadcast, serves everyone.
func TestSnapshotJoinersTakePartsOnlyWhenEveryoneCan(t *testing.T) {
	a := colouredSession(t, 10, 0)
	old := a.buf.OldestSeq()
	a.noteSnapshotJoiner("v-new", snapCapParts, old)
	if j := a.takeSnapshotJoiners(); len(j) != 1 || j[0] != "v-new" {
		t.Fatalf("a joiner that asked: %v", j)
	}
	a.noteSnapshotJoiner("v-new", snapCapParts, old)
	a.noteSnapshotJoiner("", "", old) // an older viewer, also owed one
	if j := a.takeSnapshotJoiners(); j != nil {
		t.Fatalf("with an older viewer owed a snapshot too: parts to %v", j)
	}
	a.noteSnapshotJoiner("v-new", "", old) // asked for nothing
	if j := a.takeSnapshotJoiners(); j != nil {
		t.Fatalf("a viewer that did not ask: parts to %v", j)
	}
	a.noteSnapshotJoiner("v-new", snapCapParts, a.buf.NextSeq()) // caught up: owed none
	if j := a.takeSnapshotJoiners(); j != nil {
		t.Fatalf("a caught-up resume: parts to %v", j)
	}
}

// No byte cap configured (REMINAL_SCROLLBACK_BYTES=0): the line cap alone,
// never the one-frame cap.
func TestASnapshotInPartsWithNoByteCapIsNotCutToAFrame(t *testing.T) {
	a := colouredSession(t, 4000, 0)
	parts, _, _ := partsFor(t, a)
	if got := numberedLines(unpackParts(t, a, parts)); got != 4000 {
		t.Fatalf("with no byte cap the parts carry %d/4000 lines", got)
	}
}

// History that compresses badly goes in several parts, each under 640 KiB
// encrypted, and they join back to the whole.
func TestASnapshotThatCompressesBadlyGoesInSeveralParts(t *testing.T) {
	a := colouredSessionPal(t, 4000, 0, true)
	parts, _, err := partsFor(t, a)
	if err != nil || len(parts) < 2 {
		t.Fatalf("random-colour history: %d parts (%v), want several", len(parts), err)
	}
	if got := numberedLines(unpackParts(t, a, parts)); got != 4000 {
		t.Fatalf("%d parts joined carry %d/4000 lines", len(parts), got)
	}
	t.Logf("random-colour history: %d parts, sizes up to 640 KiB each", len(parts))
}
