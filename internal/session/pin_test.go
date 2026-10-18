// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"strings"
	"testing"
)

// A forwarded port's PIN is 12 characters of Crockford base32, every one of
// them used, none of them one a person could read as another.
func TestExposePINIsLongBase32(t *testing.T) {
	seen := map[rune]bool{}
	for i := 0; i < 400; i++ {
		pin, err := NewExposePIN()
		if err != nil {
			t.Fatal(err)
		}
		if len(pin) != ExposePINLength {
			t.Fatalf("PIN %q is %d characters, want %d", pin, len(pin), ExposePINLength)
		}
		for _, r := range pin {
			if !strings.ContainsRune(exposeAlphabet, r) {
				t.Fatalf("PIN %q has %q, not in the alphabet", pin, r)
			}
			seen[r] = true
		}
	}
	if len(seen) != len(exposeAlphabet) {
		t.Fatalf("400 PINs used %d of the %d characters", len(seen), len(exposeAlphabet))
	}
	if strings.ContainsAny(exposeAlphabet, "ILOU") {
		t.Fatal("the alphabet has a character that reads as another")
	}
}
