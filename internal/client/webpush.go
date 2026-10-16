// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"reminal/internal/config"
)

// Phone alerts ride Web Push, and the message is sealed HERE, on the machine,
// to the phone's own subscription keys (RFC 8291, aes128gcm). The relay only
// adds the VAPID signature every push service demands and forwards the blob,
// so neither it nor Apple/Google/Mozilla can read "battery 12% on
// MacBook-Pro" — the same promise the terminal stream makes.
//
// Why the relay is involved at all: a phone holds ONE push subscription per
// site, bound to one VAPID key, so every machine has to push under the same
// key — which therefore lives with the site, not with each machine.

// pushSub is one phone's subscription as the browser's PushManager hands it
// out: the push-service URL plus the two keys the payload is sealed to.
type pushSub struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"` // base64url, uncompressed P-256 point (65 bytes)
	Auth     string `json:"auth"`   // base64url, 16 bytes
}

// pushRecordSize is the aes128gcm record size advertised in the header. One
// record carries the whole message; alerts are a few hundred bytes.
const pushRecordSize = 4096

// maxPushPlaintext keeps a sealed message under the 4096 bytes every push
// service accepts (header 86 + tag 16 + padding delimiter 1).
const maxPushPlaintext = 3000

func b64urlDecode(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	// Some browsers have been seen handing out standard base64; accept both.
	return base64.RawStdEncoding.DecodeString(s)
}

// sealPush encrypts plaintext for one subscription (RFC 8291 §3, RFC 8188).
func sealPush(sub pushSub, plaintext []byte) ([]byte, error) {
	if len(plaintext) > maxPushPlaintext {
		return nil, errors.New("push message too large")
	}
	uaPubRaw, err := b64urlDecode(sub.P256dh)
	if err != nil || len(uaPubRaw) != 65 {
		return nil, errors.New("subscription key is not a P-256 point")
	}
	authSecret, err := b64urlDecode(sub.Auth)
	if err != nil || len(authSecret) != 16 {
		return nil, errors.New("subscription auth secret is not 16 bytes")
	}
	curve := ecdh.P256()
	uaPub, err := curve.NewPublicKey(uaPubRaw)
	if err != nil {
		return nil, fmt.Errorf("subscription key: %w", err)
	}
	asPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPubRaw := asPriv.PublicKey().Bytes()
	ecdhSecret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}

	// IKM = HKDF(auth_secret, ecdh_secret, "WebPush: info" 0x00 ua_pub as_pub, 32)
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPubRaw...), asPubRaw...)
	ikm, err := hkdf.Key(sha256.New, ecdhSecret, authSecret, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// A single, final record: plaintext followed by the 0x02 delimiter.
	record := append(append([]byte{}, plaintext...), 0x02)
	ct := gcm.Seal(nil, nonce, record, nil)

	var out bytes.Buffer
	out.Write(salt)
	_ = binary.Write(&out, binary.BigEndian, uint32(pushRecordSize))
	out.WriteByte(byte(len(asPubRaw)))
	out.Write(asPubRaw)
	out.Write(ct)
	return out.Bytes(), nil
}

// pushMessage is what the phone's service worker renders. Plain data — the
// worker draws it with showNotification, nothing in it is ever executed.
type pushMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	// Tag collapses repeats: a second "battery low" replaces the first on the
	// lock screen instead of stacking under it.
	Tag string `json:"tag,omitempty"`
	URL string `json:"url,omitempty"`
	// At is when the machine saw the change (unix ms). Push services do not
	// promise order, so the phone uses it to keep a late "unplugged" from
	// replacing the "connected" that happened after it, and to say when an
	// alert that arrives late actually happened.
	At int64 `json:"at,omitempty"`
}

// errPushGone means the push service no longer knows this subscription (the
// phone unsubscribed, cleared site data, or the browser rotated it). The only
// correct response is to forget it.
var errPushGone = errors.New("push subscription is gone")

// errPushUnbound: the relay did not take this alert's token or signature as
// this machine's (a token that is not, or no longer, good). The token is
// dropped, so the phone's page offers to renew it.
var errPushUnbound = errors.New("this phone's alerts are not bound to this machine; renew them from the phone")

// errPushClock: the relay found this alert's time too far from its own,
// even after this machine set its clock by the relay's.
var errPushClock = errors.New("the relay found this machine's clock off")

// errPushRenew: this phone's alerts here have no token (they were turned on
// before alerts were bound to their machine, or the relay refused theirs),
// so none is sent until the phone renews them; the page offers to.
var errPushRenew = errors.New("this phone's alerts need renewing: open reminal on the phone and tap Renew alerts on this phone")

// relayClock is how far the relay's clock is from this machine's, as the
// relay last said (the Date of each answer, or the time a "clock" refusal
// gives): an alert's time is the relay's, so a machine whose clock is off
// still has its alerts taken.
var relayClock struct {
	sync.Mutex
	off time.Duration
}

func relayNow() time.Time {
	relayClock.Lock()
	defer relayClock.Unlock()
	return time.Now().Add(relayClock.off)
}

// learnRelayClock sets the offset from an answer of the relay's: its own
// time in ms (exact, a "clock" refusal gives it) or else its Date (to the
// second, which an alert's time allows for).
func learnRelayClock(resp *http.Response, nowMs int64) {
	var at time.Time
	if nowMs > 0 {
		at = time.UnixMilli(nowMs)
	} else if d, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		at = d
	} else {
		return
	}
	relayClock.Lock()
	relayClock.off = time.Until(at)
	relayClock.Unlock()
}

var pushHTTP = &http.Client{Timeout: 20 * time.Second}

// pushURL is where sealed messages are handed to the relay. A var so tests can
// aim it at a stub.
var pushURL = func() string { return config.WebURL() + "/push" }

// sendPush seals msg for sub and hands it to the relay to sign and forward.
// alertSigned is what a machine signs for one alert (the relay's
// alertSigned, cloudflare/src/push.ts).
func alertSigned(endpoint string, payloadSum []byte, ts int64) string {
	return fmt.Sprintf("reminal-push\n%s\n%x\n%d", endpoint, payloadSum, ts)
}

// sendPush sends one alert with the phone's token (pushEntry.Token),
// signed by this machine's key over where it goes, what it is and when (the
// relay's time) — what the relay checks the token against. Without a token
// nothing is sent (errPushRenew). Refused for its time, it is sent once
// more on the relay's time as that refusal gives it.
func sendPush(sub pushSub, token string, msg pushMessage) error {
	if token == "" {
		return errPushRenew
	}
	pt, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	sealed, err := sealPush(sub, pt)
	if err != nil {
		return err
	}
	key, err := loadOrCreateMachineKey()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(sealed)
	var out struct {
		Status int    `json:"status"`
		Error  string `json:"error"`
		Code   string `json:"code"`
		Now    int64  `json:"now"`
	}
	for try := 0; ; try++ {
		ts := relayNow().Unix()
		body, _ := json.Marshal(map[string]any{
			"endpoint": sub.Endpoint,
			"payload":  base64.StdEncoding.EncodeToString(sealed),
			"ttl":      3600,
			"urgency":  "high",
			"token":    token,
			"ts":       ts,
			"machine":  base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)),
			"sig":      base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(alertSigned(sub.Endpoint, sum[:], ts)))),
		})
		req, err := http.NewRequest(http.MethodPost, pushURL(), bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := pushHTTP.Do(req)
		if err != nil {
			return err
		}
		out.Code, out.Now, out.Error, out.Status = "", 0, "", 0
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&out)
		resp.Body.Close()
		learnRelayClock(resp, out.Now)
		if resp.StatusCode == http.StatusForbidden {
			switch out.Code {
			case "unbound":
				return errPushUnbound
			case "clock":
				if try == 0 {
					continue // on the relay's time, as it just said
				}
				return errPushClock
			}
		}
		if resp.StatusCode != http.StatusOK {
			if out.Error != "" {
				return fmt.Errorf("relay refused the alert: %s", out.Error)
			}
			return fmt.Errorf("relay refused the alert (HTTP %d)", resp.StatusCode)
		}
		break
	}
	switch {
	case out.Status == http.StatusNotFound || out.Status == http.StatusGone:
		return errPushGone
	case out.Status >= 200 && out.Status < 300:
		return nil
	default:
		return fmt.Errorf("push service answered %d", out.Status)
	}
}
