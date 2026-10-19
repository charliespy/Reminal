// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// reminal's entry in Codex's config gets a tool timeout Codex's 60 s default
// would not give a call that waits for its answer: added, raised, or left as
// the person set it when theirs is longer.
func TestCodexEntryGetsAToolTimeout(t *testing.T) {
	entry := "[mcp_servers.reminal]\ncommand = \"/r\"\nargs = [\"mcp\"]\n"
	for _, c := range []struct {
		name, in, want string
	}{
		{"added after the entry's keys",
			"model = \"o3\"\n\n" + entry + "\n[mcp_servers.other]\ncommand = \"/x\"\n",
			"model = \"o3\"\n\n" + entry + "tool_timeout_sec = 180\n\n[mcp_servers.other]\ncommand = \"/x\"\n"},
		{"added at the end of the file",
			entry,
			entry + "tool_timeout_sec = 180\n"},
		{"added when the file has no final newline",
			"[mcp_servers.reminal]\ncommand = \"/r\"",
			"[mcp_servers.reminal]\ncommand = \"/r\"\ntool_timeout_sec = 180\n"},
		{"added to the entry, not to its env table",
			entry + "\n[mcp_servers.reminal.env]\nX = \"1\"\n",
			entry + "tool_timeout_sec = 180\n\n[mcp_servers.reminal.env]\nX = \"1\"\n"},
		{"a quoted table name",
			"[mcp_servers.\"reminal\"]\ncommand = \"/r\"\n",
			"[mcp_servers.\"reminal\"]\ncommand = \"/r\"\ntool_timeout_sec = 180\n"},
		{"a lower one raised in place",
			"[mcp_servers.reminal]\ncommand = \"/r\"\ntool_timeout_sec = 60\nargs = [\"mcp\"]\n",
			"[mcp_servers.reminal]\ncommand = \"/r\"\ntool_timeout_sec = 180\nargs = [\"mcp\"]\n"},
		{"a lower float raised",
			"[mcp_servers.reminal]\ntool_timeout_sec = 90.5 # mine\n",
			"[mcp_servers.reminal]\ntool_timeout_sec = 180\n"},
		{"CRLF kept",
			"[mcp_servers.reminal]\r\ncommand = \"/r\"\r\n\r\n[x]\r\n",
			"[mcp_servers.reminal]\r\ncommand = \"/r\"\r\ntool_timeout_sec = 180\r\n\r\n[x]\r\n"},
	} {
		got, changed := withCodexToolTimeout(c.in, codexToolTimeoutSec)
		if !changed || got != c.want {
			t.Errorf("%s:\n got %q (changed %v)\nwant %q", c.name, got, changed, c.want)
		}
		if again, changed := withCodexToolTimeout(got, codexToolTimeoutSec); changed || again != got {
			t.Errorf("%s: a second run changed it again: %q", c.name, again)
		}
	}
	for _, c := range []struct{ name, in string }{
		{"a higher one kept", "[mcp_servers.reminal]\ntool_timeout_sec = 600\n"},
		{"exactly 180 kept", "[mcp_servers.reminal]\ntool_timeout_sec = 180\n"},
		{"one that is not a number left alone", "[mcp_servers.reminal]\ntool_timeout_sec = \"x\"\n"},
		{"another server's timeout is not ours", "[mcp_servers.reminal-two]\ncommand = \"/r\"\n"},
		{"no reminal entry", "model = \"o3\"\n[mcp_servers.other]\ncommand = \"/x\"\n"},
		{"empty", ""},
	} {
		if got, changed := withCodexToolTimeout(c.in, codexToolTimeoutSec); changed || got != c.in {
			t.Errorf("%s: changed to %q", c.name, got)
		}
	}
}

// On disk: the file keeps its mode, the first version is backed up, and a
// home with no Codex config is left as it is.
func TestSetCodexToolTimeoutOnDisk(t *testing.T) {
	home := t.TempDir()
	if err := setCodexToolTimeout(home, codexToolTimeoutSec); err != nil {
		t.Fatalf("no config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatal("a home with no Codex config was given one")
	}
	path := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	orig := "[mcp_servers.reminal]\ncommand = \"/r\"\n"
	if err := os.WriteFile(path, []byte(orig), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := setCodexToolTimeout(home, codexToolTimeoutSec); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != orig+"tool_timeout_sec = 180\n" {
		t.Fatalf("config: %q", b)
	}
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v, want 0640", fi.Mode().Perm())
	}
	if bak, _ := os.ReadFile(path + ".bak"); string(bak) != orig {
		t.Fatalf("backup: %q", bak)
	}
}

// `codex mcp add` writes reminal's entry over, dropping a tool_timeout_sec the
// person set; a longer one of theirs is put back, a shorter one raised, and a
// failed add changes nothing more.
func TestCodexAddKeepsALongerTimeoutItsAddDrops(t *testing.T) {
	path := ""
	codexAdd := func() error { // as `codex mcp add` writes the entry
		return os.WriteFile(path, []byte("model = \"o3\"\n\n[mcp_servers.reminal]\ncommand = \"/r\"\nargs = [\"mcp\"]\n"), 0o600)
	}
	for _, c := range []struct {
		had  string
		want string
	}{
		{"", "tool_timeout_sec = 180"},
		{"tool_timeout_sec = 30\n", "tool_timeout_sec = 180"},
		{"tool_timeout_sec = 600\n", "tool_timeout_sec = 600"},
		{"tool_timeout_sec = 900.5\n", "tool_timeout_sec = 900.5"},
	} {
		home := t.TempDir()
		path = filepath.Join(home, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("[mcp_servers.reminal]\ncommand = \"/old\"\n"+c.had), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := codexAddWithToolTimeout(home, codexAdd); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(path)
		if want := "model = \"o3\"\n\n[mcp_servers.reminal]\ncommand = \"/r\"\nargs = [\"mcp\"]\n" + c.want + "\n"; string(b) != want {
			t.Errorf("had %q:\n got %q\nwant %q", c.had, b, want)
		}
	}
	home := t.TempDir()
	failed := errors.New("codex: no")
	if err := codexAddWithToolTimeout(home, func() error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("a failed add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatal("a failed add still wrote a config")
	}
}
