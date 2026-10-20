// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strconv"
	"strings"
)

// cursorRow is the screen's row at its cursor, as drawn — its SGR colours and
// its OSC 8 hyperlinks kept (what the program wrote and the emulator parsed,
// its URLs unable to hold the OSC's ends), its trailing blanks not — and the
// cursor's column. Only ever at a shell: never a program's own screen, which
// repaints by rows it counted.
func (a *Agent) cursorRow() (string, int, bool) {
	a.screenMu.Lock()
	defer a.screenMu.Unlock()
	if a.screen == nil {
		return "", 0, false
	}
	p := a.screen.CursorPosition()
	rows := strings.Split(a.screen.Render(), "\n")
	if p.Y < 0 || p.Y >= len(rows) {
		return "", 0, false
	}
	return strings.TrimRight(rows[p.Y], " "), p.X, true
}

// redrawRow is row (cursorRow's) drawn again on the line a notice's end left
// the cursor on, the cursor back at col: the shell's input line is the
// cursor's again, as a shell has it after a job's notice. Nothing at col 0
// (no line was interrupted).
func redrawRow(row string, col int) string {
	if col <= 0 {
		return ""
	}
	return "\x1b[0m" + row + "\x1b[0m\r\x1b[" + strconv.Itoa(col+1) + "G"
}

// recordAtShell writes a notice line (text, with no line ends of its own)
// to a terminal at its shell, leaving the shell's input line the cursor's:
// on an empty row at col 0 (nothing drawn yet: the prompt is still to come),
// the line and a line end, the prompt drawn after it; else on a line of its
// own below what is there, and that row drawn again under it, the cursor
// back at its column (redrawRow). Never a notice the shell's prompt waits
// under on an empty row, where the keys typed at it never match the line
// they show on.
func (a *Agent) recordAtShell(text string) {
	row, col, ok := a.cursorRow()
	if ok && col == 0 && row == "" {
		a.record([]byte(text + "\r\n"))
		return
	}
	out := "\r\n" + text + "\r\n"
	if ok {
		out += redrawRow(row, col)
	}
	a.record([]byte(out))
}
