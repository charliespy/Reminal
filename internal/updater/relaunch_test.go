// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package updater

import (
	"os"
	"testing"
)

// The reminal started after an upgrade isn't offered the upgrade again, and
// nothing it starts inherits the mark.
func TestJustUpgradedIsReadOnceAndNotPassedOn(t *testing.T) {
	t.Setenv(justUpgradedEnv, "1")
	if !justUpgraded() {
		t.Fatal("the mark was not seen")
	}
	if _, ok := os.LookupEnv(justUpgradedEnv); ok {
		t.Fatal("the mark is still in the environment")
	}
	if justUpgraded() {
		t.Fatal("the mark was seen twice")
	}
}

func TestStartUpgradedNeedsAPath(t *testing.T) {
	if err := startUpgraded(""); err == nil {
		t.Fatal("started with no path")
	}
}
