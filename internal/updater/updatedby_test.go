// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package updater

import (
	"strings"
	"testing"
)

func TestUpdatedByName(t *testing.T) {
	for _, c := range []struct{ env, want string }{
		{"", ""},
		{"   ", ""},
		{" an image ", "an image"},
		{"an\x1b[31mimage\n", "an[31mimage"},
		{strings.Repeat("x", 80), strings.Repeat("x", 60)},
	} {
		t.Setenv(UpdatedByEnv, c.env)
		if got := UpdatedBy(); got != c.want {
			t.Errorf("UpdatedBy() with %q = %q, want %q", c.env, got, c.want)
		}
	}
}

// Updated by what installed it: it never checks, never offers and never
// replaces itself, by any route — and none of them touches the network
// first (releasesURL and the feed point nowhere reachable here).
func TestUpdatedByNeverChecksOrReplaces(t *testing.T) {
	t.Setenv(UpdatedByEnv, "an image")
	old := releasesURL
	releasesURL = "http://127.0.0.1:1/never"
	t.Cleanup(func() { releasesURL = old })
	availMu.Lock()
	availFor = "" // no memo of another test's answer
	availMu.Unlock()

	if shouldCheck("2.0.33") {
		t.Error("shouldCheck: a build updated by its image checks for updates")
	}
	if got := Available("2.0.33"); got != "" {
		t.Errorf("Available = %q, want no offer", got)
	}
	want := "updated by an image"
	if got := UpgradeBlockedReason("2.0.33"); !strings.Contains(got, want) {
		t.Errorf("UpgradeBlockedReason = %q, want it to say %q", got, want)
	}
	if up, err := Upgrade("2.0.33"); up || err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("Upgrade = %v, %v; want refused, saying %q", up, err, want)
	}
	if up, err := UpgradeQuiet("2.0.33"); up || err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("UpgradeQuiet = %v, %v; want refused, saying %q", up, err, want)
	}
	if v, err := SwitchChannel("http://127.0.0.1:1/releases.json", "other"); v != "" || err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("SwitchChannel = %q, %v; want refused, saying %q", v, err, want)
	}

	// Nor does the macOS bundle heal put reminal.app in place of the loose
	// binary its installer put there.
	if bundleHealAllowed("2.0.33") {
		t.Error("bundleHealAllowed: a build updated by its image would be replaced by a bundle")
	}

	// Unset, the same build is offered and may be replaced as before.
	t.Setenv(UpdatedByEnv, "")
	if !shouldCheck("2.0.33") || UpgradeBlockedReason("2.0.33") != "" {
		t.Error("unset: a release build is still kept from updating itself")
	}
	if !bundleHealAllowed("2.0.33") || bundleHealAllowed("dev") {
		t.Error("unset: the bundle heal is not as before (a release build heals, a dev build does not)")
	}
}
