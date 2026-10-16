// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"reminal/internal/protocol"
)

// enrolledPhone is a phone of an owner enrolled on this machine, with alerts
// on: the owner's key, and the phone's subscription.
func enrolledPhone(t *testing.T, endpoint string) (ed25519.PrivateKey, pushSub) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := AddOwner(ownerID(pub), "phone"); err != nil {
		t.Skipf("cannot enrol an owner in this environment: %v", err)
	}
	sub, _, _ := newTestSub(t, endpoint)
	if err := upsertPushSub(base64.StdEncoding.EncodeToString(pub), sub, defaultPushRules()); err != nil {
		t.Fatal(err)
	}
	return priv, sub
}

// The owner's proof for handing a token on covers the token itself: a proof
// for one token cannot be spent handing this machine another.
func TestATokensProofCoversTheToken(t *testing.T) {
	isolateHome(t)
	if got := pushAction("token", "https://e", "T"); got != "push:token:https://e:T" {
		t.Errorf("pushAction = %q (the page signs push:token:<endpoint>:<token>)", got)
	}
	if got := pushAction("set", "https://e", "T"); got != "push:set:https://e" {
		t.Errorf("pushAction(set) = %q", got)
	}
	priv, sub := enrolledPhone(t, "https://fcm.googleapis.com/fcm/send/a")
	a := &Agent{sessionID: "S1"}
	const other = "v1.1792108800.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	send := func(signed, token string) string {
		resp := &protocol.DirResponse{}
		a.applyLocalPush(resp, &pushReq{Op: "token", Sub: sub, Token: token,
			Proof: signAction(t, priv, "S1", pushAction("token", sub.Endpoint, signed), time.Now().Unix())})
		return resp.Push.Error
	}
	if why := send(testPushToken, other); why == "" {
		t.Error("a proof for one token handed this machine another")
	}
	if e, _ := findPushSub(sub.Endpoint); e.NewToken != "" || e.Token != "" {
		t.Fatalf("a token was kept: %+v", e)
	}
	if why := send(testPushToken, testPushToken); why != "" {
		t.Fatalf("the token its proof names was refused: %s", why)
	}
	if e, _ := findPushSub(sub.Endpoint); e.NewToken != testPushToken {
		t.Errorf("token not kept: %+v", e)
	}
}

// push.json is written by more than one process (the directory host on an
// owner's request, the watcher dropping a dead phone or a refused token):
// each update is made whole, under a lock both take, so none is lost.
func TestPushStoreWritersInTwoProcessesLoseNothing(t *testing.T) {
	const n = 40
	if who := os.Getenv("REMINAL_PUSH_WRITER"); who != "" {
		// The parent's home (TestMain gave this process one of its own).
		t.Setenv("HOME", os.Getenv("REMINAL_PUSH_WRITER_HOME"))
		for i := 0; i < n; i++ {
			sub, _, _ := newTestSub(t, fmt.Sprintf("https://fcm.googleapis.com/fcm/send/%s%d", who, i))
			if err := upsertPushSub("dev", sub, defaultPushRules()); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	home := isolateHome(t)
	child := exec.Command(os.Args[0], "-test.run=^TestPushStoreWritersInTwoProcessesLoseNothing$", "-test.count=1")
	child.Env = append(os.Environ(), "REMINAL_PUSH_WRITER=c", "REMINAL_PUSH_WRITER_HOME="+home)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		sub, _, _ := newTestSub(t, fmt.Sprintf("https://fcm.googleapis.com/fcm/send/p%d", i))
		if err := upsertPushSub("dev", sub, defaultPushRules()); err != nil {
			t.Fatal(err)
		}
	}
	if err := child.Wait(); err != nil {
		t.Fatalf("the other writer: %v", err)
	}
	subs, err := loadPushSubs()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2*n {
		t.Errorf("%d phones kept of %d written by two processes at once", len(subs), 2*n)
	}
}

// reminal doctor's Phone alerts line: phones to renew (before the date, and
// after it), none counted of an owner removed from this machine, nothing to
// renew where the relay does not bind alerts (an older one), and a clock
// the relay would refuse bound alerts for.
func TestDoctorSaysHowPhoneAlertsStand(t *testing.T) {
	isolateHome(t)
	binds, skew := true, time.Duration(0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().Add(-skew).UTC().Format(http.TimeFormat))
		if r.URL.Path == "/push/token" && binds {
			w.WriteHeader(http.StatusMethodNotAllowed)
			_, _ = w.Write([]byte(`{"error":"POST only"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	t.Setenv("REMINAL_WEB", srv.URL)
	defer func(d time.Time) { pushUntokenedUntil = d }(pushUntokenedUntil)
	pushUntokenedUntil = time.Now().Add(30 * 24 * time.Hour)
	expect := func(name string, want level, words string) {
		t.Helper()
		lvl, msg := checkPhoneAlerts()
		if lvl != want || !strings.Contains(msg, words) {
			t.Errorf("%s: %v %q; want %v with %q", name, lvl, msg, want, words)
		}
	}
	expect("no phones", levelOK, "no phones")
	// A removed owner's phone: nothing to renew.
	sub, _, _ := newTestSub(t, "https://fcm.googleapis.com/fcm/send/gone")
	if err := upsertPushSub(base64.StdEncoding.EncodeToString(make([]byte, 32)), sub, defaultPushRules()); err != nil {
		t.Fatal(err)
	}
	expect("a removed owner's phone", levelOK, "no phones")
	_, mine := enrolledPhone(t, "https://fcm.googleapis.com/fcm/send/mine")
	expect("before the date", levelWarn, "1 phone(s) need alerts renewed before")
	binds = false
	expect("an older relay", levelOK, "this relay does not bind alerts")
	binds = true
	pushUntokenedUntil = time.Now().Add(-time.Hour)
	expect("after the date", levelWarn, "have not got alerts from this machine since")
	if err := setPushToken(mine.Endpoint, testPushToken); err != nil {
		t.Fatal(err)
	}
	expect("renewed", levelOK, "none to renew")
	skew = 5 * time.Minute
	expect("a clock off", levelWarn, "clock is 5m")
}
