// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// After the date the relay stops forwarding untokened alerts, a new token
// the relay refuses still costs the phone nothing: that alert goes with the
// phone's working token, which stays; the refused one is dropped.
func TestAfterTheGraceARefusedNewTokenStillDelivers(t *testing.T) {
	isolateHome(t)
	defer func(d time.Time) { pushUntokenedUntil = d }(pushUntokenedUntil)
	pushUntokenedUntil = time.Now().Add(-time.Hour)
	sub, _, _ := newTestSub(t, "https://fcm.googleapis.com/fcm/send/a")
	if err := upsertPushSub(base64.StdEncoding.EncodeToString(make([]byte, 32)), sub, defaultPushRules()); err != nil {
		t.Fatal(err)
	}
	const junk = "v1.1792108800.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		tok, _ := body["token"].(string)
		sent = append(sent, tok)
		switch {
		case tok == "":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "renew", "code": "renew"})
		case tok != testPushToken:
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no", "code": "unbound"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]int{"status": 201})
		}
	}))
	defer srv.Close()
	old := pushURL
	pushURL = func() string { return srv.URL + "/push" }
	defer func() { pushURL = old }()
	if err := setPushToken(sub.Endpoint, testPushToken); err != nil {
		t.Fatal(err)
	}
	e, _ := findPushSub(sub.Endpoint)
	if err := sendBound(e, pushMessage{Title: "t"}); err != nil {
		t.Fatal(err)
	}
	if err := setPushToken(sub.Endpoint, junk); err != nil {
		t.Fatal(err)
	}
	sent = nil
	e, _ = findPushSub(sub.Endpoint)
	if err := sendBound(e, pushMessage{Title: "t"}); err != nil {
		t.Fatalf("the alert was lost: %v", err)
	}
	e, _ = findPushSub(sub.Endpoint)
	if fmt.Sprint(sent) != fmt.Sprint([]string{junk, testPushToken}) || e.Token != testPushToken || e.NewToken != "" {
		t.Errorf("sent %q, token %q, new %q; want the refused one then the working one, and only the working one kept", sent, e.Token, e.NewToken)
	}
}
