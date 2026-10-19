// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"slices"
	"testing"

	"reminal/internal/session"
)

// A tunnel with a long PIN says so, so the relay's gate holds wrong PINs per
// source only; one carried over with a six-digit PIN does not.
func TestTunnelTellsTheRelayItsPINIsLong(t *testing.T) {
	long, err := session.NewExposePIN()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(tunnelCaps(long), "long_pin") {
		t.Fatalf("a %d-character PIN: caps %v lack long_pin", len(long), tunnelCaps(long))
	}
	if slices.Contains(tunnelCaps("123456"), "long_pin") {
		t.Fatal("a six-digit PIN claimed long_pin")
	}
	for _, c := range []string{"req_chunk", "ws_subproto"} {
		if !slices.Contains(tunnelCaps("123456"), c) {
			t.Fatalf("lost %s", c)
		}
	}
}
