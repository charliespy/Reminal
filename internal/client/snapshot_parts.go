// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"

	"github.com/gorilla/websocket"

	"reminal/internal/protocol"
)

// A joining viewer's snapshot in parts. The plain snapshot is one TypeData
// frame, so its history is cut to what fits the relay's 1 MiB message once
// encrypted (maxSnapshotPlaintext): a viewer opening a long session saw its
// newest ~1,200 lines, while one already open had them all. A viewer that
// says it can (Snap "gz+parts" on its resume) is sent the whole history the
// agent keeps for snapshots (scrollbackBytes), gzip-compressed and split into
// parts that each fit a message, addressed to it alone: agent messages reach
// every viewer, and a part — not a self-contained paint — must reach only the
// viewer assembling it. The address is inside the encrypted part (its
// header's "to"), not on the message: a sealed frame carries no more than its
// type, counters, seq and payload. It paints once, with every part in.

const (
	// snapCapParts is the capability a viewer's resume names.
	snapCapParts = "gz+parts"
	// snapshotPartBytes is a part's share of the gzip stream: encrypted and
	// base64'd (4/3, plus nonce, tag and header) it stays under 640 KiB.
	snapshotPartBytes = 470 << 10
)

// noteSnapshotJoiner records, from a resume, who will be owed a snapshot and
// how it can take one: only a resume whose cursor is older than the buffer
// gets one (runSender).
func (a *Agent) noteSnapshotJoiner(viewer, snap string, cursor uint64) {
	if a.buf == nil || cursor > a.buf.OldestSeq() {
		return
	}
	a.snapMu.Lock()
	defer a.snapMu.Unlock()
	if snap == snapCapParts && viewer != "" {
		if a.snapJoiners == nil {
			a.snapJoiners = map[string]bool{}
		}
		a.snapJoiners[viewer] = true
		return
	}
	a.snapPlain = true
}

// takeSnapshotJoiners is who to send parts to for the snapshot now owed, and
// clears the record. None when anyone owed it cannot take parts: the plain
// frame, broadcast, serves every joiner, those that could take parts too.
func (a *Agent) takeSnapshotJoiners() []string {
	a.snapMu.Lock()
	defer a.snapMu.Unlock()
	joiners, plain := a.snapJoiners, a.snapPlain
	a.snapJoiners, a.snapPlain = nil, false
	if plain || len(joiners) == 0 {
		return nil
	}
	out := make([]string, 0, len(joiners))
	for v := range joiners {
		out = append(out, v)
	}
	return out
}

// snapshotParts builds the snapshot (history within scrollbackBytes), gzips
// it and cuts it into parts, encrypted for each joiner (its id in each part's
// header), and returns them by joiner with the seq they cover. None: nothing
// to send.
func (a *Agent) snapshotParts(joiners []string) (map[string][]string, uint64, error) {
	if a.screen == nil || a.box == nil || len(joiners) == 0 {
		return nil, 0, nil
	}
	a.screenMu.Lock()
	history := a.snapshotHistory()
	// The configured cap on a snapshot's history (REMINAL_SCROLLBACK_BYTES;
	// 0: none, the line cap only) — not the one-frame cap the plain
	// snapshot is held to.
	budget := a.scrollbackBytes
	if budget <= 0 {
		budget = 1 << 40
	}
	snap := buildSnapshot(a.screen, history, nil, budget, false)
	latest := a.buf.LatestSeq()
	a.screenMu.Unlock()
	if snap == "" {
		return nil, 0, nil
	}
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestSpeed)
	if _, err := w.Write([]byte(snap)); err != nil {
		return nil, 0, err
	}
	if err := w.Close(); err != nil {
		return nil, 0, err
	}
	var idb [8]byte
	_, _ = rand.Read(idb[:])
	id := hex.EncodeToString(idb[:])
	body := gz.Bytes()
	n := (len(body) + snapshotPartBytes - 1) / snapshotPartBytes
	out := make(map[string][]string, len(joiners))
	for _, to := range joiners {
		parts := make([]string, 0, n)
		for i := 0; i < n; i++ {
			chunk := body[i*snapshotPartBytes : min((i+1)*snapshotPartBytes, len(body))]
			hdr, _ := json.Marshal(struct {
				ID string `json:"id"`
				I  int    `json:"i"`
				N  int    `json:"n"`
				To string `json:"to"`
			}{id, i, n, to})
			pt := make([]byte, 4, 4+len(hdr)+len(chunk))
			binary.BigEndian.PutUint32(pt, uint32(len(hdr)))
			pt = append(append(pt, hdr...), chunk...)
			enc, err := a.box.Encrypt(pt)
			if err != nil {
				return nil, 0, err
			}
			parts = append(parts, enc)
		}
		out[to] = parts
	}
	return out, latest, nil
}

// sendSnapshotParts sends the snapshot in parts to each joiner, and returns
// the seq it covers (0: nothing sent).
func (a *Agent) sendSnapshotParts(conn *websocket.Conn, joiners []string) (uint64, error) {
	byJoiner, latest, err := a.snapshotParts(joiners)
	if err != nil || len(byJoiner) == 0 {
		return 0, err
	}
	for _, v := range joiners {
		for _, p := range byJoiner[v] {
			if err := a.writeMsg(conn, protocol.Message{Type: protocol.TypeSnapshotPart, Seq: latest, Data: p}); err != nil {
				return 0, err
			}
		}
	}
	return latest, nil
}
