// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"unicode/utf8"
)

// What a restored session types for its agent is built from the agent's own
// command line and conversation id, saved while it ran. Quoting keeps a
// shell's syntax out of it, but a terminal acts on control characters before
// any shell sees a quote, so none may be typed at all.

// hasControl says s holds a C0 or C1 control character (or is not valid
// UTF-8, which a terminal may read as one).
func hasControl(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, c := range s {
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c <= 0x9f) {
			return true
		}
	}
	return false
}

// shellUnsafe is what may not be typed into shell, whatever its quoting:
// control characters anywhere, and on cmd a %, which it expands even inside
// quotes.
func shellUnsafe(shell string) func(string) bool {
	if isCmdShell(shell) {
		return func(s string) bool { return hasControl(s) || strings.Contains(s, "%") }
	}
	return hasControl
}

func isCmdShell(shell string) bool {
	base := strings.ToLower(shell[strings.LastIndexAny(shell, `/\`)+1:])
	return strings.HasPrefix(base, "cmd")
}

// noteSafe is s fit to print into a terminal: its control characters
// dropped, and at most max runes.
func noteSafe(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, c := range strings.ToValidUTF8(s, "") {
		// Control characters, and the bidirectional overrides and isolates,
		// which would show the line's text in another order than it is
		// (review of #273).
		if c < 0x20 || c == 0x7f || (c >= 0x80 && c <= 0x9f) || (c >= 0x202a && c <= 0x202e) || (c >= 0x2066 && c <= 0x2069) {
			continue
		}
		if n == max {
			b.WriteString("…")
			break
		}
		b.WriteRune(c)
		n++
	}
	return b.String()
}

// psQuotes are the characters PowerShell ends a single-quoted string at: '
// and its typographic forms, each doubled to stand for itself.
var psQuotes = strings.NewReplacer("'", "''", "‘", "‘‘", "’", "’’", "‚", "‚‚", "‛", "‛‛")
