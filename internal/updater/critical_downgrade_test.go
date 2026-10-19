// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package updater

import (
	"testing"
	"time"
)

// A forced-upgrade floor above this build says how soon to upgrade, never
// to what: a latest no newer than this build is not installed over it — not
// an older one (a downgrade), not the same one again.
func TestAForcedUpgradeNeverInstallsAnOlderBuild(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	follow(t, "alpha", "crit.json")

	for _, tc := range []struct {
		latest       string
		wantTag      string
		wantCritical bool
	}{
		{latest: "v2.0.30", wantTag: "", wantCritical: false},       // older: never
		{latest: "v2.0.34", wantTag: "", wantCritical: false},       // this one: nothing to do
		{latest: "v2.0.36", wantTag: "v2.0.36", wantCritical: true}, // newer, past the floor: forced
	} {
		writeCache(cacheEntry{CheckedAt: time.Now(), LatestTag: tc.latest, AssetURL: "https://example.test/b", CriticalMin: "v2.0.35"})
		tag, _, critical, err := check("v2.0.34", time.Second)
		if err != nil {
			t.Fatalf("latest %s: %v", tc.latest, err)
		}
		if tag != tc.wantTag || critical != tc.wantCritical {
			t.Fatalf("latest %s with a floor of v2.0.35 on v2.0.34: got (%q, critical %v), want (%q, %v)", tc.latest, tag, critical, tc.wantTag, tc.wantCritical)
		}
	}
}
