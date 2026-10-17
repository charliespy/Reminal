// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every file the module embeds (//go:embed, in any package, test embeds
// included) is checked out exactly as it is stored: .gitattributes marks it
// -text, or eol=lf. A Windows checkout converts text files to CRLF
// (core.autocrlf), so an embedded file it converted would ship in a Windows
// build as other bytes than every other build's: a viewer whose scripts no
// longer match their hashes, an extension that is not the one reminal writes
// elsewhere. A new //go:embed outside the paths already covered fails here
// until .gitattributes covers it.
func TestEveryEmbeddedFileIsCheckedOutAsStored(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skip("not in a git checkout: nothing a checkout converts to check")
	}
	dir := strings.TrimSpace(string(root))

	list := exec.Command("go", "list", "-json", "./...")
	list.Dir = dir
	out, err := list.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var files []string
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			Dir                                         string
			EmbedFiles, TestEmbedFiles, XTestEmbedFiles []string
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("go list output: %v", err)
		}
		for _, group := range [][]string{p.EmbedFiles, p.TestEmbedFiles, p.XTestEmbedFiles} {
			for _, f := range group {
				rel, err := filepath.Rel(dir, filepath.Join(p.Dir, f))
				if err != nil {
					t.Fatal(err)
				}
				files = append(files, filepath.ToSlash(rel))
			}
		}
	}
	if len(files) == 0 {
		t.Fatal("go list found no embedded files; the check would pass on nothing")
	}
	sort.Strings(files)

	// git check-attr -z: path NUL attribute NUL value NUL, for each.
	check := exec.Command("git", append([]string{"check-attr", "-z", "text", "eol", "--"}, files...)...)
	check.Dir = dir
	attrs, err := check.Output()
	if err != nil {
		t.Fatalf("git check-attr: %v", err)
	}
	text, eol := map[string]string{}, map[string]string{}
	fields := strings.Split(strings.TrimSuffix(string(attrs), "\x00"), "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		switch fields[i+1] {
		case "text":
			text[fields[i]] = fields[i+2]
		case "eol":
			eol[fields[i]] = fields[i+2]
		}
	}
	var bad []string
	for _, f := range files {
		if text[f] != "unset" && eol[f] != "lf" {
			bad = append(bad, f+" (text: "+text[f]+", eol: "+eol[f]+")")
		}
	}
	if len(bad) > 0 {
		t.Errorf("embedded, but a Windows checkout may convert them (mark them -text in .gitattributes):\n  %s", strings.Join(bad, "\n  "))
		return
	}
	t.Logf("%d embedded files, each checked out as stored", len(files))
}
