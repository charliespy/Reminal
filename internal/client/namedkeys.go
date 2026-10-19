// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"fmt"
	"strings"
)

// MaxNamedKeys caps the keys one send_keys call presses by name: each goes
// as its own delivery, a moment apart.
const MaxNamedKeys = 8

// namedKeyBytes is what each named key sends to a terminal.
var namedKeyBytes = map[string]string{
	"esc":       "\x1b",
	"escape":    "\x1b",
	"tab":       "\t",
	"shift-tab": "\x1b[Z",
	"enter":     "\r",
	"return":    "\r",
	"space":     " ",
	"backspace": "\x7f",
	"delete":    "\x1b[3~",
	"del":       "\x1b[3~",
	"up":        "\x1b[A",
	"down":      "\x1b[B",
	"right":     "\x1b[C",
	"left":      "\x1b[D",
	"home":      "\x1b[H",
	"end":       "\x1b[F",
	"pageup":    "\x1b[5~",
	"pagedown":  "\x1b[6~",
}

// NamedKeys turns key names ("ctrl-c", "esc", "up", ...), separated by spaces
// or commas, into the bytes each key sends, one entry per key. An agent often
// cannot put a raw control character in its tool call (it is stripped, or a
// \u0003 it writes arrives as six characters), so a key it means is named.
//
// Each key is its own delivery: a run of them sent as one could read as a
// paste to a full-screen app (see Agent.wrapPaste), and an Esc followed at
// once by more bytes reads as Alt and the next key.
func NamedKeys(spec string) ([][]byte, error) {
	names := KeyNames(spec)
	if len(names) > MaxNamedKeys {
		return nil, fmt.Errorf("key names %d keys; at most %d at a time", len(names), MaxNamedKeys)
	}
	out := make([][]byte, 0, len(names))
	for _, name := range names {
		b, ok := namedKey(name)
		if !ok {
			return nil, fmt.Errorf("unknown key %q: use ctrl-a … ctrl-z, esc, tab, shift-tab, enter, space, backspace, delete, up, down, left, right, home, end, pageup, pagedown", name)
		}
		out = append(out, b)
	}
	return out, nil
}

func namedKey(name string) ([]byte, bool) {
	n := strings.ToLower(name)
	if b, ok := namedKeyBytes[n]; ok {
		return []byte(b), true
	}
	// ctrl-c, ctrl+c, c-c and ^c are all Ctrl-C.
	for _, p := range []string{"ctrl-", "ctrl+", "control-", "c-", "^"} {
		if rest, ok := strings.CutPrefix(n, p); ok {
			if len(rest) == 1 {
				switch c := rest[0]; {
				case c >= 'a' && c <= 'z':
					return []byte{c - 'a' + 1}, true
				case c == '[':
					return []byte{0x1b}, true
				case c == '\\':
					return []byte{0x1c}, true
				case c == ']':
					return []byte{0x1d}, true
				}
			}
			return nil, false
		}
	}
	return nil, false
}

// KeyNames splits a key spec into its names: "esc, esc" is two.
func KeyNames(spec string) []string {
	return strings.FieldsFunc(spec, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
}
