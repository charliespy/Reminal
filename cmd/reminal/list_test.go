// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"reminal/internal/session"
)

// listOutput is what `reminal list` prints for args.
func listOutput(t *testing.T, args ...string) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	runErr := runList(args)
	os.Stdout = stdout
	w.Close()
	out, _ := io.ReadAll(r)
	if runErr != nil {
		t.Fatalf("runList: %v", runErr)
	}
	return string(out)
}

// A shared port is reached by its link, so `reminal list` shows it under the
// port's row. Its PIN stays out of the plain list, as a shell's does.
func TestListShowsAPortsLink(t *testing.T) {
	isolateHome(t)
	t.Setenv("REMINAL_SESSION", "")
	writeRec(t, session.Active{ID: "AAAA1111", PIN: "111111", Name: "build", OpenURL: "https://live.example/?s=AAAA1111"})
	writeRec(t, session.Active{ID: "BBBB2222", PIN: "222222", Kind: session.KindPort, Port: 3000, OpenURL: "https://port-bbbb2222.example/"})

	out := listOutput(t)
	if !strings.Contains(out, "https://port-bbbb2222.example/") {
		t.Errorf("the port's link isn't listed:\n%s", out)
	}
	if strings.Contains(out, "https://live.example/?s=AAAA1111") {
		t.Errorf("a shell's link is listed too (only ports get one):\n%s", out)
	}
	if strings.Contains(out, "222222") || strings.Contains(out, "111111") {
		t.Errorf("a PIN is in the plain list:\n%s", out)
	}
}
