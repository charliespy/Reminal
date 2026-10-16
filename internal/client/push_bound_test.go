// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"
)

const testPushToken = "v1.1792108800.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// An alert to a phone that has this machine's token carries it, and this
// machine's signature over where it goes, what it is and when; to a phone
// without a token nothing is sent: it needs renewing.
func TestAnAlertIsSignedByItsMachine(t *testing.T) {
	isolateHome(t)
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body)
		_ = json.NewEncoder(w).Encode(map[string]int{"status": 201})
	}))
	defer srv.Close()
	old := pushURL
	pushURL = func() string { return srv.URL + "/push" }
	defer func() { pushURL = old }()
	sub, _, _ := newTestSub(t, "https://fcm.googleapis.com/fcm/send/x")
	if err := sendPush(sub, testPushToken, pushMessage{Title: "t", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if err := sendPush(sub, "", pushMessage{Title: "t", Body: "b"}); !errors.Is(err, errPushRenew) {
		t.Fatalf("untokened: %v, want errPushRenew", err)
	}
	if len(got) != 1 {
		t.Fatalf("%d requests", len(got))
	}
	b := got[0]
	pub, _ := MachinePub()
	if b["token"] != testPushToken || b["machine"] != base64.StdEncoding.EncodeToString(pub) {
		t.Fatalf("token or machine: %v", b)
	}
	ts := int64(b["ts"].(float64))
	if d := time.Since(time.Unix(ts, 0)); d < -time.Minute || d > time.Minute {
		t.Errorf("ts %d is not now", ts)
	}
	payload, _ := base64.StdEncoding.DecodeString(b["payload"].(string))
	sig, _ := base64.StdEncoding.DecodeString(b["sig"].(string))
	sum := sha256.Sum256(payload)
	if !ed25519.Verify(pub, []byte(alertSigned(sub.Endpoint, sum[:], ts)), sig) {
		t.Error("the signature is not this machine's over the alert")
	}
}

// The page hands a phone's token on (op "token"): kept with that phone,
// reported back (tokened), and anything not shaped as a token, or for a
// phone with no alerts here, refused.
func TestAPhonesTokenIsKeptWithItsAlerts(t *testing.T) {
	isolateHome(t)
	sub, _, _ := newTestSub(t, "https://fcm.googleapis.com/fcm/send/a")
	if err := setPushToken(sub.Endpoint, testPushToken); err == nil {
		t.Error("a token kept for a phone with no alerts here")
	}
	if err := upsertPushSub("dev", sub, defaultPushRules()); err != nil {
		t.Fatal(err)
	}
	if n := untokenedPushSubs(); n != 1 {
		t.Errorf("untokened %d, want 1", n)
	}
	for _, bad := range []string{"", "v1.1.short", "v2.1792108800.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", testPushToken + "\n"} {
		if err := setPushToken(sub.Endpoint, bad); err == nil {
			t.Errorf("%q kept", bad)
		}
	}
	if err := setPushToken(sub.Endpoint, testPushToken); err != nil {
		t.Fatal(err)
	}
	// Re-subscribed with the same endpoint (new rules): the token stays.
	if err := upsertPushSub("dev", sub, defaultPushRules()); err != nil {
		t.Fatal(err)
	}
	if e, ok := findPushSub(sub.Endpoint); !ok || e.NewToken != testPushToken {
		t.Fatalf("token after re-subscribe: %+v", e)
	}
	if n := untokenedPushSubs(); n != 0 {
		t.Errorf("untokened %d, want 0", n)
	}
}

// A token handed on is tried with the next alert and kept only once the
// relay takes it: one the relay refuses as not this machine's is dropped and
// the phone's working token stays, so a token planted on the phone costs it
// nothing. A token the relay no longer takes is dropped and that alert is
// lost: the phone then has no token here (the page offers to renew it), and
// is sent nothing until it does.
func TestATokenTheRelayRefusesIsDropped(t *testing.T) {
	isolateHome(t)
	sub, _, _ := newTestSub(t, "https://fcm.googleapis.com/fcm/send/a")
	if err := upsertPushSub(base64.StdEncoding.EncodeToString(make([]byte, 32)), sub, defaultPushRules()); err != nil {
		t.Fatal(err)
	}
	const junk = "v1.1792108800.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	good := testPushToken
	var sent []string // the token of each alert the relay was handed
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tok, _ := body["token"].(string)
		sent = append(sent, tok)
		if tok != good {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no", "code": "unbound"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"status": 201})
	}))
	defer srv.Close()
	old := pushURL
	pushURL = func() string { return srv.URL + "/push" }
	defer func() { pushURL = old }()
	step := func(name string, wantErr error, wantSent []string, wantToken, wantNew string) {
		t.Helper()
		sent = nil
		e, _ := findPushSub(sub.Endpoint)
		if err := sendBound(e, pushMessage{Title: "t"}); !errors.Is(err, wantErr) {
			t.Errorf("%s: err %v, want %v", name, err, wantErr)
		}
		e, _ = findPushSub(sub.Endpoint)
		if fmt.Sprint(sent) != fmt.Sprint(wantSent) || e.Token != wantToken || e.NewToken != wantNew {
			t.Errorf("%s: sent %q, token %q new %q; want sent %q, token %q new %q", name, sent, e.Token, e.NewToken, wantSent, wantToken, wantNew)
		}
	}
	if err := setPushToken(sub.Endpoint, good); err != nil {
		t.Fatal(err)
	}
	step("a good new token is kept", nil, []string{good}, good, "")
	if err := setPushToken(sub.Endpoint, junk); err != nil {
		t.Fatal(err)
	}
	step("a planted token is dropped, the good one stays", nil, []string{junk, good}, good, "")
	step("then the good one alone", nil, []string{good}, good, "")
	good = "v1.1792108800.CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	step("no longer taken: dropped, that alert lost", errPushUnbound, []string{testPushToken}, "", "")
	if n := untokenedPushSubs(); n != 1 {
		t.Errorf("the phone is not marked for renewal (%d untokened)", n)
	}
	step("then nothing is sent until it renews", errPushRenew, nil, "", "")
}

// The machine and the relay agree on what is signed (cloudflare/src/push.ts).
func TestAlertsAgreeWithTheRelay(t *testing.T) {
	src, err := os.ReadFile("../../cloudflare/src/push.ts")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`"reminal-push\\n" \+ endpoint \+ "\\n" \+ payloadSha256Hex \+ "\\n" \+ ts`).Match(src) {
		t.Error("push.ts's alertSigned is not the machine's: reminal-push, endpoint, sha256 hex, ts")
	}
	if got := alertSigned("https://e", []byte{0xab, 0x01}, 7); got != "reminal-push\nhttps://e\nab01\n7" {
		t.Errorf("alertSigned = %q", got)
	}
}
