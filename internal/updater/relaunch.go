// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package updater

import (
	"errors"
	"os"
)

// justUpgradedEnv marks a reminal the one before it started right after
// installing it: it has just been checked for, so it isn't offered again.
const justUpgradedEnv = "REMINAL_JUST_UPGRADED"

// justUpgraded reports whether the previous reminal started this one after
// upgrading, and takes the mark off, so nothing started from here (the
// session's shell, a reminal run in it) inherits it.
func justUpgraded() bool {
	v := os.Getenv(justUpgradedEnv)
	_ = os.Unsetenv(justUpgradedEnv)
	return v != ""
}

// startUpgraded runs the reminal now at bin (this one's path, read before the
// upgrade replaced what is there) with this one's arguments, in this one's
// terminal, so what was asked for goes ahead on the new version. Exiting
// instead would close a terminal that runs reminal itself, such as an editor's
// reminal tab. It returns only when the new one could not be started.
func startUpgraded(bin string) error {
	if bin == "" {
		return errors.New("no path to start the new version from")
	}
	return startInPlace(bin, os.Args, append(os.Environ(), justUpgradedEnv+"=1"))
}
