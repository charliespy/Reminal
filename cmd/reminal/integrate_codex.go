// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"reminal/internal/atomicfile"
)

// codexToolTimeoutSec is how long Codex lets one reminal tool call run.
// Codex's own default is 60 s, and a call that waits for its answer can take
// longer: Codex then cuts it off with an error of its own.
const codexToolTimeoutSec = 180

var (
	codexServerHeader = regexp.MustCompile(`^\s*\[\s*mcp_servers\s*\.\s*(?:` + mcpServerName + `|"` + mcpServerName + `")\s*\]\s*(?:#.*)?$`)
	codexToolTimeout  = regexp.MustCompile(`^\s*tool_timeout_sec\s*=\s*([^#]*?)\s*(?:#.*)?$`)
)

func codexConfig(home string) string { return filepath.Join(home, ".codex", "config.toml") }

// codexAddWithToolTimeout runs `codex mcp add` (add) and then gives reminal's
// entry a tool_timeout_sec of codexToolTimeoutSec, or the one the person had
// set when theirs was longer: `codex mcp add` writes the entry over and drops
// it, so it is read first.
func codexAddWithToolTimeout(home string, add func() error) error {
	want := float64(codexToolTimeoutSec)
	if raw, err := os.ReadFile(codexConfig(home)); err == nil {
		if v, ok := codexToolTimeoutIn(string(raw)); ok && v > want {
			want = v
		}
	}
	if err := add(); err != nil {
		return err
	}
	return setCodexToolTimeout(home, want)
}

// setCodexToolTimeout gives reminal's entry in Codex's config.toml (the one
// `codex mcp add` wrote) a tool_timeout_sec of at least want. A file with no
// such entry is not touched.
func setCodexToolTimeout(home string, want float64) error {
	path := codexConfig(home)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	out, changed := withCodexToolTimeout(string(raw), want)
	if !changed {
		return nil
	}
	if err := backupOnce(path, raw); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	return atomicfile.Write(path, []byte(out), mode)
}

// codexEntry finds reminal's table in the file's lines: its header line and
// the end of its own keys (the next table header — its env subtable is one,
// and a key after that header is not the entry's). start is -1 when there is
// no such table.
func codexEntry(lines []string) (start, end int) {
	start = -1
	for i, l := range lines {
		if codexServerHeader.MatchString(strings.TrimSuffix(l, "\r")) {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, 0
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			return start, i
		}
	}
	return start, end
}

// codexTimeoutAt is the tool_timeout_sec line in lines[start+1:end]: its
// index (-1 when there is none) and its value, ok when it is a number.
func codexTimeoutAt(lines []string, start, end int) (at int, v float64, ok bool) {
	for i := start + 1; i < end; i++ {
		m := codexToolTimeout.FindStringSubmatch(strings.TrimSuffix(lines[i], "\r"))
		if m == nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], "_", ""), 64)
		return i, v, err == nil
	}
	return -1, 0, false
}

// codexToolTimeoutIn is the tool_timeout_sec reminal's entry has, ok when it
// has one that is a number.
func codexToolTimeoutIn(raw string) (float64, bool) {
	lines := strings.Split(raw, "\n")
	start, end := codexEntry(lines)
	if start < 0 {
		return 0, false
	}
	at, v, ok := codexTimeoutAt(lines, start, end)
	return v, at >= 0 && ok
}

// withCodexToolTimeout is setCodexToolTimeout on the file's text: the setting
// added when the entry has none, raised when it has a lower one, and left
// alone when it is as high or not a number.
func withCodexToolTimeout(raw string, want float64) (string, bool) {
	lines := strings.Split(raw, "\n")
	start, end := codexEntry(lines)
	if start < 0 {
		return raw, false
	}
	line := "tool_timeout_sec = " + strconv.FormatFloat(want, 'f', -1, 64)
	if at, v, ok := codexTimeoutAt(lines, start, end); at >= 0 {
		if !ok || v >= want {
			return raw, false
		}
		if strings.HasSuffix(lines[at], "\r") {
			line += "\r"
		}
		lines[at] = line
		return strings.Join(lines, "\n"), true
	}
	cr := ""
	if strings.Contains(raw, "\r\n") {
		cr = "\r"
	}
	// After the entry's last key, before the blank lines that part it from
	// what follows.
	at := end
	for at > start+1 && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	if at == len(lines) {
		// The entry ends the file without a final newline.
		lines[at-1] += cr
		return strings.Join(append(lines, line+cr), "\n") + "\n", true
	}
	lines = append(lines[:at], append([]string{line + cr}, lines[at:]...)...)
	return strings.Join(lines, "\n"), true
}
