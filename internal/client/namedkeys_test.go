// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import "testing"

func TestNamedKeys(t *testing.T) {
	for name, want := range map[string]string{
		"ctrl-c": "\x03", "CTRL-C": "\x03", "ctrl+d": "\x04", "^u": "\x15", "c-z": "\x1a",
		"ctrl-a": "\x01", "ctrl-[": "\x1b", `ctrl-\`: "\x1c",
		"esc": "\x1b", "Escape": "\x1b", "tab": "\t", "shift-tab": "\x1b[Z", "enter": "\r",
		"backspace": "\x7f", "up": "\x1b[A", "left": "\x1b[D", "pagedown": "\x1b[6~",
	} {
		got, err := NamedKeys(name)
		if err != nil || len(got) != 1 || string(got[0]) != want {
			t.Errorf("%q: %q %v, want %q", name, got, err, want)
		}
	}
	if got, err := NamedKeys(""); err != nil || len(got) != 0 {
		t.Errorf("empty: %q %v", got, err)
	}
	for _, bad := range []string{"ctrl-", "ctrl-cc", "ctrl-1", "f1", "^", "\x03", "ctrl c"} {
		if _, err := NamedKeys(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if _, err := NamedKeys("up up up up up up up up up"); err == nil {
		t.Error("nine keys taken")
	}
}
