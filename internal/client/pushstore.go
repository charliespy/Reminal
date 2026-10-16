// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"reminal/internal/atomicfile"
)

// The phones that asked this machine for alerts, and what each one wants to
// hear about. Kept on the machine, not on the relay: the relay is stateless
// for push (it signs and forwards), so it never holds a list of anyone's
// devices, and removing an owner here is enough to silence that owner's phone.

// pushRules is one phone's thresholds for this machine. Each rule is off
// unless its switch is on, so an empty struct asks for nothing.
type pushRules struct {
	CPU     bool `json:"cpu"`
	CPUPct  int  `json:"cpu_pct"`  // alert at or above this utilisation…
	CPULow  int  `json:"cpu_low"`  // …or at or below this one (0 = no lower bound)…
	CPUMins int  `json:"cpu_mins"` // …held for this many minutes
	Battery bool `json:"battery"`
	BatPct  int  `json:"bat_pct"` // alert when discharging at or below this
	BatTime bool `json:"bat_time"`
	BatMins int  `json:"bat_mins"` // alert when the OS estimates this many minutes left, or fewer
	Charger bool `json:"charger"`  // alert on plugged in / unplugged
}

// defaultPushRules is what a phone gets the first time it subscribes: the two
// alerts nobody wants to miss, and the chatty charger one left for the owner
// to turn on.
func defaultPushRules() pushRules {
	return pushRules{CPU: true, CPUPct: 90, CPUMins: 5, Battery: true, BatPct: 20}
}

// clamp keeps rules inside ranges that make sense, whatever the page sent.
func (r pushRules) clamp() pushRules {
	lim := func(v, lo, hi, def int) int {
		if v == 0 {
			return def
		}
		return max(lo, min(hi, v))
	}
	r.CPUPct = lim(r.CPUPct, 10, 100, 90)
	r.CPUMins = lim(r.CPUMins, 1, 120, 5)
	// The lower bound must sit under the upper one, or the machine would be
	// "out of range" at every reading.
	if r.CPULow < 0 || r.CPULow >= r.CPUPct {
		r.CPULow = 0
	}
	r.BatPct = lim(r.BatPct, 1, 99, 20)
	r.BatMins = lim(r.BatMins, 5, 600, 60)
	return r
}

func (r pushRules) any() bool { return r.CPU || r.Battery || r.BatTime || r.Charger }

// pushEntry is one subscribed phone. DevicePub is the owner key that asked,
// so an owner removed from this machine stops getting alerts from it.
type pushEntry struct {
	DevicePub string    `json:"device_pub"` // base64 ed25519
	Sub       pushSub   `json:"sub"`
	Rules     pushRules `json:"rules"`
	Added     time.Time `json:"added"`
	// Token binds this phone's alerts to this machine: the relay sent it to
	// the phone, and the phone's page handed it here (op "token"). Sent with
	// each alert, with this machine's signature over it. "" for a phone that
	// subscribed before tokens: its alerts go untokened until
	// pushUntokenedUntil, and the page offers to renew it.
	Token string `json:"token,omitempty"`
	// NewToken is one handed on that no alert has used yet. It is tried
	// first, becomes Token once the relay takes an alert with it, and is
	// dropped if the relay refuses it; until then Token goes on working, so
	// a token that is not the relay's never costs the phone its alerts.
	NewToken string `json:"new_token,omitempty"`
}

// pushUntokenedUntil is when the relay stops forwarding alerts without a
// token (cloudflare/src/push.ts's UNTOKENED_UNTIL: the same moment).
var pushUntokenedUntil = time.Date(2026, time.December, 15, 0, 0, 0, 0, time.UTC)

// pushTokenShape is a token as the relay makes it; anything else is refused
// before it is stored.
var pushTokenShape = regexp.MustCompile(`^v1\.[0-9]{1,12}\.[A-Za-z0-9_-]{43}$`)

// setPushToken keeps the token the phone was sent for this machine, to try
// with the next alert (NewToken).
func setPushToken(endpoint, token string) error {
	if !pushTokenShape.MatchString(token) {
		return errors.New("not an alerts token")
	}
	found := false
	err := updatePushSubs(func(subs []pushEntry) []pushEntry {
		for i := range subs {
			if subs[i].Sub.Endpoint == endpoint {
				found = true
				if subs[i].Token != token {
					subs[i].NewToken = token
				}
			}
		}
		return subs
	})
	if err == nil && !found {
		err = errors.New("this phone has no alerts on this machine")
	}
	return err
}

// clearPushToken drops a phone's token the relay no longer takes — only that
// one, so a newer token handed on meanwhile stays.
func clearPushToken(endpoint, token string) error {
	return updatePushSubs(func(subs []pushEntry) []pushEntry {
		for i := range subs {
			if subs[i].Sub.Endpoint == endpoint && subs[i].Token == token {
				subs[i].Token = ""
			}
		}
		return subs
	})
}

// promotePushToken keeps a new token the relay took an alert with as the
// phone's token.
func promotePushToken(endpoint, token string) error {
	return updatePushSubs(func(subs []pushEntry) []pushEntry {
		for i := range subs {
			if subs[i].Sub.Endpoint == endpoint && subs[i].NewToken == token {
				subs[i].Token, subs[i].NewToken = token, ""
			}
		}
		return subs
	})
}

// dropNewPushToken forgets a new token the relay refused; the phone's
// token, if it has one, stays.
func dropNewPushToken(endpoint, token string) error {
	return updatePushSubs(func(subs []pushEntry) []pushEntry {
		for i := range subs {
			if subs[i].Sub.Endpoint == endpoint && subs[i].NewToken == token {
				subs[i].NewToken = ""
			}
		}
		return subs
	})
}

// pushSubsToRenew is how many phones of owners still enrolled here get
// untokened alerts; -1 when no phone gets alerts from this machine.
func pushSubsToRenew() int {
	pushStoreMu.Lock()
	subs, _ := loadPushSubs()
	pushStoreMu.Unlock()
	n, any := 0, false
	for _, s := range subs {
		if !s.stillOwner() {
			continue
		}
		any = true
		if s.Token == "" && s.NewToken == "" {
			n++
		}
	}
	if !any {
		return -1
	}
	return n
}

// untokenedPushSubs is how many phones still get untokened alerts.
func untokenedPushSubs() int {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	subs, _ := loadPushSubs()
	n := 0
	for _, s := range subs {
		if s.Token == "" && s.NewToken == "" {
			n++
		}
	}
	return n
}

type pushFile struct {
	Version int         `json:"version"`
	Subs    []pushEntry `json:"subs"`
	// Machine is this machine's public key (MachinePub) when the file was
	// written: the phones are this machine's, not its files'. A copy of the
	// home that comes up as another machine (another key) reads as having
	// no phones, whatever made the copy, and each phone asks that machine
	// again; one that comes back as the same machine keeps them. "" in a
	// file written before this: kept, and stamped at its next write.
	Machine string `json:"machine,omitempty"`
}

// thisMachine is this machine's key as push.json records it.
func thisMachine() (string, error) {
	pub, err := MachinePub()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

// pushStoreMu serialises read-modify-write of push.json within a process,
// and pushStoreLock across processes: a phone's token written by one while
// another rewrites the file for a dead subscription must not be lost (its
// alerts would stop once untokened ones do). Each write is whole
// (atomicfile): a crash leaves the old file or the new one.
var pushStoreMu sync.Mutex

const pushStoreLock = "push.lock"

func pushStorePath() (string, error) {
	dir, err := reminalDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "push.json"), nil
}

func loadPushSubs() ([]pushEntry, error) {
	path, err := pushStorePath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f pushFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Machine != "" {
		me, err := thisMachine()
		if err != nil {
			return nil, err
		}
		if f.Machine != me {
			return nil, nil // another machine's phones
		}
	}
	return f.Subs, nil
}

func savePushSubs(subs []pushEntry) error {
	path, err := pushStorePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	me, err := thisMachine()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(pushFile{Version: 1, Subs: subs, Machine: me}, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, b, 0o600)
}

// updatePushSubs applies fn to the stored list under the lock and saves it.
func updatePushSubs(fn func([]pushEntry) []pushEntry) error {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	lock, err := lockFile(pushStoreLock)
	if err != nil {
		return err
	}
	defer unlockFile(lock)
	subs, err := loadPushSubs()
	if err != nil {
		return err
	}
	return savePushSubs(fn(subs))
}

// findPushSub returns the entry for endpoint, if this machine has one.
func findPushSub(endpoint string) (pushEntry, bool) {
	pushStoreMu.Lock()
	defer pushStoreMu.Unlock()
	subs, _ := loadPushSubs()
	for _, s := range subs {
		if s.Sub.Endpoint == endpoint {
			return s, true
		}
	}
	return pushEntry{}, false
}

// upsertPushSub records (or re-records) one phone's subscription and rules.
// Keyed by endpoint: one phone, one entry, whichever owner key it used.
func upsertPushSub(devicePub string, sub pushSub, rules pushRules) error {
	return updatePushSubs(func(subs []pushEntry) []pushEntry {
		for i := range subs {
			if subs[i].Sub.Endpoint == sub.Endpoint {
				subs[i].DevicePub, subs[i].Sub, subs[i].Rules = devicePub, sub, rules
				return subs
			}
		}
		return append(subs, pushEntry{DevicePub: devicePub, Sub: sub, Rules: rules, Added: time.Now()})
	})
}

func removePushSub(endpoint string) error {
	return updatePushSubs(func(subs []pushEntry) []pushEntry {
		out := subs[:0]
		for _, s := range subs {
			if s.Sub.Endpoint != endpoint {
				out = append(out, s)
			}
		}
		return out
	})
}

// stillOwner reports whether the device that subscribed is still enrolled.
// Checked at send time rather than on owner removal, so every removal path
// (CLI, revoke-self, a hand-edited owners.json) silences the phone alike.
func (e pushEntry) stillOwner() bool {
	pub, err := base64.StdEncoding.DecodeString(e.DevicePub)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	ok, err := IsOwner(pub)
	return err == nil && ok
}

// validPushSub rejects anything that could not be a real push subscription
// before it is stored; the relay re-checks the endpoint before it forwards.
func validPushSub(s pushSub) error {
	if !strings.HasPrefix(s.Endpoint, "https://") || len(s.Endpoint) > 1024 {
		return errors.New("not a push endpoint")
	}
	if k, err := b64urlDecode(s.P256dh); err != nil || len(k) != 65 {
		return errors.New("bad subscription key")
	}
	if a, err := b64urlDecode(s.Auth); err != nil || len(a) != 16 {
		return errors.New("bad subscription secret")
	}
	return nil
}
