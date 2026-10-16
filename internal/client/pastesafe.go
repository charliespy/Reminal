// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// plainText is s as something safe to print: without control characters
// (C0, C1, DEL) or invisible formatting ones (such as bidi overrides), and no
// longer than max runes. Names and messages that come from another machine or
// the relay go through it before they reach a terminal.
func plainText(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if n == max {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// pastedPrefix marks a received name that would otherwise land as a hidden
// file or folder, such as .git, .ssh or .bashrc.
const pastedPrefix = "pasted-"

// receivedName is the name a pasted file or folder is saved under: the
// sender's, as plain text, with fallback for one that isn't a plain name
// here (empty, "..", or a reserved name on Windows), and pastedPrefix in
// front of one that starts with a dot. changed says whether it differs.
func receivedName(name, fallback string) (string, bool) {
	clean := plainText(filepath.Base(name), 200)
	if clean == "" || clean == "." || clean == ".." || !filepath.IsLocal(clean) || strings.ContainsAny(clean, `/\`) {
		return fallback, true
	}
	if strings.HasPrefix(clean, ".") {
		return pastedPrefix + clean, true
	}
	return clean, clean != name
}

// isGitName reports whether a path component is a .git folder or file, as
// this file system would see it: case-insensitively, and with Windows'
// trailing dots and spaces ignored.
func isGitName(c string) bool {
	return strings.EqualFold(strings.TrimRight(withoutFormatChars(c), ". "), ".git")
}

// withoutFormatChars is s without invisible formatting characters (such as
// zero-width ones), which some file systems ignore in names.
func withoutFormatChars(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

// renameGit renames any .git component of rel, so a pasted folder never
// carries a working repository (its config and hooks run commands). For the
// first one renamed, it reports the path up to it, before and after, or "".
func renameGit(rel string) (renamed, from, to string) {
	parts := strings.Split(rel, string(filepath.Separator))
	orig := append([]string(nil), parts...)
	for i, c := range parts {
		if isGitName(c) {
			parts[i] = pastedPrefix + c
			if from == "" {
				from, to = filepath.Join(orig[:i+1]...), filepath.Join(parts[:i+1]...)
			}
		}
	}
	return filepath.Join(parts...), from, to
}
