// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"testing"
	"time"
)

// flatRow is a row as the limit notices are read: lowercase, no whitespace.
func flatRow(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), "")) }

// When a limit resets, from the ways the harnesses say it: a clock time
// (with a zone, a date, or neither), a wait, an instant.
func TestParseLimitReset(t *testing.T) {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("no zone data")
	}
	london, _ := time.LoadLocation("Europe/London")
	now := time.Date(2026, 10, 19, 10, 0, 0, 0, la) // a Monday morning in LA
	for _, tc := range []struct {
		row  string
		want time.Time
	}{
		{"resets 1:30pm", time.Date(2026, 10, 19, 13, 30, 0, 0, la)},
		{"resets 9am", time.Date(2026, 10, 20, 9, 0, 0, 0, la)}, // past today: tomorrow
		{"resets 3pm (America/Los_Angeles)", time.Date(2026, 10, 19, 15, 0, 0, 0, la)},
		{"resets at 2pm (Europe/London)", time.Date(2026, 10, 20, 14, 0, 0, 0, london)}, // 18:00 there now
		{"Your limit will reset at 5pm (America/Los_Angeles).", time.Date(2026, 10, 19, 17, 0, 0, 0, la)},
		{"resets Oct 21, 1pm", time.Date(2026, 10, 21, 13, 0, 0, 0, la)},
		{"resets Oct 21 at 1:15am (America/Los_Angeles)", time.Date(2026, 10, 21, 1, 15, 0, 0, la)},
		{"resets 13:30", time.Date(2026, 10, 19, 13, 30, 0, 0, la)},
		{"resets 12am", time.Date(2026, 10, 20, 0, 0, 0, 0, la)},
		{"resets 12pm", time.Date(2026, 10, 19, 12, 0, 0, 0, la)},
		{"try again at 3:05 PM", time.Date(2026, 10, 19, 15, 5, 0, 0, la)},
		{"try again in 2h 5m", now.Add(2*time.Hour + 5*time.Minute)},
		{"or try again in 4 days 2 hours 5 minutes.", now.Add(4*24*time.Hour + 2*time.Hour + 5*time.Minute)},
		{"Please try again in 45s", now.Add(45 * time.Second)},
		{"resets in 3 hours", now.Add(3 * time.Hour)},
		{"quota resets at 2026-10-20T14:00:00Z", time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)},
		{"available again 2026-10-20 14:00 UTC", time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)},
		{"reset 2026-10-20T07:00:00-07:00", time.Date(2026, 10, 20, 14, 0, 0, 0, time.UTC)},
		{"resets 2pm UTC", time.Date(2026, 10, 19, 14, 0, 0, 0, time.UTC).Add(24 * time.Hour)}, // 17:00 UTC now
		// Not said, or not a time.
		{"Usage limit reached", time.Time{}},
		{"try again later", time.Time{}},
		{"resets at 2", time.Time{}},
		{"resets 25:00", time.Time{}},
	} {
		got := parseLimitReset(flatRow(tc.row), now)
		if !got.Equal(tc.want) {
			t.Errorf("%q: %v, want %v", tc.row, got, tc.want)
		}
	}
}
