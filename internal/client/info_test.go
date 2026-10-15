// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reminal/internal/session"
)

// info --json carries the attention state, so a tool can tell which session
// needs you; a bare shell has none and the fields are left out entirely.
func TestInfoJSONAttn(t *testing.T) {
	since := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	b, err := json.Marshal(InfoJSON(&session.Active{ID: "ABCD2345", PIN: "123456", OpenURL: "http://r/?s=ABCD2345", Attn: "input", AttnSince: since}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Attn      string    `json:"attn"`
		AttnSince time.Time `json:"attn_since"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Attn != "input" || !got.AttnSince.Equal(since) {
		t.Fatalf("attn = %q since %v, want input since %v (%s)", got.Attn, got.AttnSince, since, b)
	}

	b, _ = json.Marshal(InfoJSON(&session.Active{ID: "ABCD2345", PIN: "123456", OpenURL: "http://r/?s=ABCD2345"}))
	if strings.Contains(string(b), "attn") {
		t.Fatalf("a bare shell should carry no attn fields: %s", b)
	}
}
