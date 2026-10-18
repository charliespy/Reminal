// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package relay

import "testing"

func TestSecretEqualMatchesOnlyTheSameCredential(t *testing.T) {
	for _, c := range []struct {
		got, want string
		same      bool
	}{
		{"tok-abc", "tok-abc", true},
		{"tok-abd", "tok-abc", false},
		{"tok-ab", "tok-abc", false},
		{"tok-abcd", "tok-abc", false},
		{"", "tok-abc", false},
		{"tok-abc", "", false},
	} {
		if got := secretEqual(c.got, c.want); got != c.same {
			t.Errorf("secretEqual(%q, %q) = %v, want %v", c.got, c.want, got, c.same)
		}
	}
}
