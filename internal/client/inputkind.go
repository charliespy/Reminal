// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"regexp"
)

// terminalReplyRe matches what a terminal sends on its own, not a person:
// answers to the program's queries (cursor position, device attributes,
// status, colours, version) and focus in/out. A program that asks every
// prompt (fish), or a window switched to and from, would otherwise read as
// someone typing into a tab nobody is at.
var terminalReplyRe = regexp.MustCompile(
	`\x1b\[\d*(;\d*)?R` + // cursor position report
		`|\x1b\[[?>=]?[\d;]*c` + // device attributes
		`|\x1b\[\??\d*(;\d*)*n` + // device status report
		`|\x1b\[[IO]` + // focus in / out
		`|\x1b\][^\x07\x1b]*(\x07|\x1b\\)` + // OSC reply (colour queries …)
		`|\x1bP[^\x1b]*\x1b\\`) // DCS reply (version, settings)

// typedByPerson says whether keys from a viewer or the host terminal hold
// anything a person sent, once the terminal's own replies are taken out.
func typedByPerson(b []byte) bool { return len(personKeys(b)) > 0 }

// personKeys are the keys in b a person sent: b without the terminal's own
// replies.
func personKeys(b []byte) []byte { return terminalReplyRe.ReplaceAll(b, nil) }

// inputLine follows whether the keys sent to a session left something typed
// and not yet sent on its input line (draft): printable keys start one; a
// Return sends it, Ctrl-C or Ctrl-U clears it. A paste is typed, its line
// ends included (they do not send it); other key sequences (arrows, function
// keys) change nothing. It errs toward a draft: a line cleared some other way
// (Esc Esc, backspacing it away) still reads as one until the next Return or
// turn, so a sleep asks rather than drops what was typed.
type inputLine struct {
	draft bool
	paste bool // inside a bracketed paste
}

func (l *inputLine) feed(b []byte) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0x1b {
			rest := b[i:]
			switch {
			case bytes.HasPrefix(rest, pasteStart):
				l.paste = true
				i += len(pasteStart) - 1
			case bytes.HasPrefix(rest, pasteEnd):
				l.paste = false
				i += len(pasteEnd) - 1
			default:
				i += escLen(b[i:]) - 1
			}
			continue
		}
		switch {
		case l.paste:
			l.draft = true
		case c == '\r' || c == '\n' || c == 0x03 || c == 0x15:
			l.draft = false
		case c >= 0x20 && c != 0x7f:
			l.draft = true
		}
	}
}

// escLen is the length of the key sequence at b (b[0] is ESC): a CSI to its
// final byte, an SS3 key, or ESC and the key pressed with Alt.
func escLen(b []byte) int {
	if len(b) < 2 {
		return 1
	}
	switch b[1] {
	case '[':
		for j := 2; j < len(b); j++ {
			if b[j] >= 0x40 && b[j] <= 0x7e {
				return j + 1
			}
		}
		return len(b)
	case 'O':
		return min(3, len(b))
	}
	return 2
}
