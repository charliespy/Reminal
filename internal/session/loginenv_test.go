// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

//go:build !windows

package session

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// The login shell is asked for what its rc sets: an rc that prints a banner
// does not run into a value, an unset name is left out, and a name that is
// not one is never put to the shell.
func TestLoginEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/sh")
	rc := "echo welcome back\nexport AGENT_DIR=/srv/agent\nexport EMPTY_ONE=\n"
	if err := os.WriteFile(filepath.Join(home, ".profile"), []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	got := LoginEnv("AGENT_DIR", "NOT_SET_ANYWHERE", "EMPTY_ONE", "X; touch pwned")
	if want := map[string]string{"AGENT_DIR": "/srv/agent", "EMPTY_ONE": ""}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if _, err := os.Stat("pwned"); err == nil {
		t.Error("a name was run as a command")
	}
}
