// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package updater

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

// UpdatedByEnv names who puts a new reminal in place on this install, when it
// is not reminal itself: an image rebuilt with each release, a package
// manager. Set, this build never checks for a newer one, never offers one and
// refuses to replace itself — a copy written over it would be undone by the
// next image, or would leave it at odds with what installed it — and says
// who does instead ("updated by <name>"), where a check would otherwise have
// failed or offered an upgrade that cannot stick.
const UpdatedByEnv = "REMINAL_UPDATED_BY"

// UpdatedBy is the name in UpdatedByEnv: printable, at most 60 characters,
// "" when unset (reminal updates itself).
func UpdatedBy() string {
	s := strings.TrimSpace(os.Getenv(UpdatedByEnv))
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
	if r := []rune(s); len(r) > 60 {
		s = strings.TrimSpace(string(r[:60]))
	}
	return s
}

// updatedByRefusal is the error a replace refuses with on such an install,
// nil when it may go ahead.
func updatedByRefusal() error {
	if by := UpdatedBy(); by != "" {
		return fmt.Errorf("this reminal is updated by %s, not from here", by)
	}
	return nil
}
