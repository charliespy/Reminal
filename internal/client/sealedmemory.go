// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"reminal/internal/crypto"
)

// A viewer offers sealed frames twice in one handshake: bound into what is
// authenticated (the PIN handshake's sid, the device's signed transcript) and
// unbound, as an agent from before the binding answers. An agent that knows
// the bound form answers it, sealed, its answer signed or keyed with the rest
// — nothing between can take that out. An answer to the unbound form cannot
// be told from one a relay brought about by stripping the bound offer, so
// this device remembers the sessions and machines it has seen seal, and
// refuses an unsealed answer from them. What it has never seen seal, it takes
// unsealed: an agent from before sealed frames still connects, and that first
// contact is the one a relay could still downgrade.

// sealedFramesRequired refuses an unsealed answer from any agent, seen to
// seal or not: the one line to set (true) once agents from before sealed
// frames are gone. A variable only so a test can hold it both ways.
var sealedFramesRequired = false

// errSealDowngrade is an unsealed answer from a session or machine this device
// has seen answer sealed.
var errSealDowngrade = errors.New("handshake: that machine answered with sealed frames before and this answer has none — refused (if its reminal was reinstalled at an older version, update it there)")

// sealedPeers is where the sessions ("s:<id>") and machines ("m:<key>") seen
// to seal are kept, with when each was last seen so.
const sealedPeersFile = "sealed-peers.json"

var sealedPeersMu sync.Mutex

func sealedPeersPath() (string, error) {
	dir, err := reminalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, sealedPeersFile), nil
}

func readSealedPeers() map[string]int64 {
	out := map[string]int64{}
	p, err := sealedPeersPath()
	if err != nil {
		return out
	}
	if b, err := os.ReadFile(p); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// sealedSeen reports whether this device has seen peer answer sealed.
func sealedSeen(peer string) bool {
	if peer == "" {
		return false
	}
	sealedPeersMu.Lock()
	defer sealedPeersMu.Unlock()
	_, ok := readSealedPeers()[peer]
	return ok
}

// sealedPeersMax is how many peers are kept, the newest: a cap on the file
// every handshake reads, never an expiry — one would reopen the downgrade.
const sealedPeersMax = 2000

// sealedPeersLock serialises the file's read-modify-write across this user's
// processes (the daemon, an MCP server, a viewer): one note never loses
// another's.
const sealedPeersLock = "sealed-peers.lock"

// noteSealed records that peer answered sealed. Best-effort: not remembering
// leaves the next first contact as open as this one was, nothing worse.
func noteSealed(peer string) {
	if peer == "" {
		return
	}
	sealedPeersMu.Lock()
	defer sealedPeersMu.Unlock()
	// Another process mid-write holds the lock a moment; past that, written
	// without it (as before the lock), rather than not at all.
	for i := 0; i < 50; i++ {
		lock, held, err := tryLockFile(sealedPeersLock)
		if err != nil {
			break
		}
		if held {
			defer unlockFile(lock)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	m := readSealedPeers()
	m[peer] = time.Now().Unix()
	if len(m) > sealedPeersMax {
		type kv struct {
			k string
			t int64
		}
		all := make([]kv, 0, len(m))
		for k, t := range m {
			all = append(all, kv{k, t})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t > all[j].t || (all[i].t == all[j].t && all[i].k < all[j].k) })
		m = map[string]int64{}
		for _, e := range all[:sealedPeersMax] {
			m[e.k] = e.t
		}
		m[peer] = time.Now().Unix()
	}
	p, err := sealedPeersPath()
	if err != nil {
		return
	}
	if b, err := json.Marshal(m); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = atomicWrite(p, b, 0o600)
	}
}

// admitSeal is the sealed-frames answer of a handshake, as this viewer takes
// it: bound (the agent answered the bound offer) must carry seal info; an
// unbound answer is taken sealed when it carries some, and unsealed only from
// a peer never seen to seal (and while sealedFramesRequired is off).
func admitSeal(sessionKey, key, exID []byte, seal string, bound bool, peer string) (viewerSeal, error) {
	if seal == "" {
		if bound {
			return viewerSeal{}, errSealMissing
		}
		if sealedFramesRequired || sealedSeen(peer) {
			return viewerSeal{}, errSealDowngrade
		}
		return viewerSeal{}, nil
	}
	st, err := newViewerSeal(sessionKey, key, exID, seal)
	if err != nil {
		return viewerSeal{}, err
	}
	noteSealed(peer)
	return st, nil
}

// pakeOpenEither finishes the PIN handshake on the agent's element with the
// bound state (the sid naming sealed frames) and then the one as before, and
// unwraps the session key with whichever key opens it: bound says which.
func pakeOpenEither(bound, plain *crypto.CPaceState, agentElem, exID, wrapped []byte) (key, sessionKey []byte, isBound bool, err error) {
	for i, st := range []*crypto.CPaceState{bound, plain} {
		k, ferr := st.Finish(agentElem)
		if ferr != nil {
			return nil, nil, false, fmt.Errorf("handshake: %w", ferr)
		}
		if sk, uerr := crypto.UnwrapSessionKey(k, exID, wrapped); uerr == nil {
			return k, sk, i == 0, nil
		}
	}
	return nil, nil, false, errors.New("handshake failed: PIN mismatch")
}

// acceptSeal is admitSeal for this viewer's connection.
func (v *Viewer) acceptSeal(sessionKey, key, exID []byte, seal string, bound bool, peer string) error {
	st, err := admitSeal(sessionKey, key, exID, seal, bound, peer)
	v.writeMu.Lock()
	v.seal = st
	v.writeMu.Unlock()
	return err
}

// ownerAnswerVerifies checks the machine's signature on an owner handshake's
// answer: over the transcript with the answer to the sealed-frames offer
// signed in (bound), or over the one as before.
func ownerAnswerVerifies(sessionID string, viewerEph, agentEph, devicePub, machinePub, machineSig []byte, seal string) (bound, ok bool) {
	pub := ed25519.PublicKey(machinePub)
	if crypto.VerifyOwner(pub, crypto.OwnerServerTranscriptFor(sessionID, viewerEph, agentEph, devicePub, machinePub, true, seal), machineSig) {
		return true, true
	}
	return false, crypto.VerifyOwner(pub, crypto.OwnerServerTranscript(sessionID, viewerEph, agentEph, devicePub, machinePub), machineSig)
}
