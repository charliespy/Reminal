// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"testing"

	"reminal/internal/updater"
)

// Who updates this install rides the machine's stats, host_info's and the
// directory's alike, so a viewer says so in place of a check.
func TestHostInfoSaysWhoUpdatesIt(t *testing.T) {
	t.Setenv(updater.UpdatedByEnv, "an image")
	if got := gatherHostInfo().UpdatedBy; got != "an image" {
		t.Errorf("host info's updated_by = %q, want %q", got, "an image")
	}
	t.Setenv(updater.UpdatedByEnv, "")
	if got := gatherHostInfo().UpdatedBy; got != "" {
		t.Errorf("unset: updated_by = %q, want none", got)
	}
}
