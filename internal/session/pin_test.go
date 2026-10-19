// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import "testing"

// A new session's PIN has eight digits, and a six-digit one from before is
// still a PIN.
func TestNewSessionPINIsEightDigits(t *testing.T) {
	pin, err := NewPIN(PINLength)
	if err != nil {
		t.Fatal(err)
	}
	if len(pin) != 8 {
		t.Fatalf("new PIN %q has %d digits, want 8", pin, len(pin))
	}
	for _, p := range []string{pin, "123456"} {
		if err := ValidatePIN(p); err != nil {
			t.Fatalf("ValidatePIN(%q): %v", p, err)
		}
	}
}
