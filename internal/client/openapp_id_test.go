// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// An app is opened only by an id the app list could have offered: on macOS a
// .app bundle in one of the folders it scans (or one folder below), on Linux
// a .desktop file in one of its folders — as on Windows, where it must be a
// shortcut in the Start Menu. Anything else (a path elsewhere, a file of
// another kind, a name that reads as an option) is refused before anything is
// run.
func TestAnAppIsOpenedOnlyByAnIdTheListOffers(t *testing.T) {
	root := t.TempDir()
	apps, else_ := filepath.Join(root, "Applications"), filepath.Join(root, "elsewhere")
	for _, d := range []string{
		filepath.Join(apps, "Safari.app"), filepath.Join(apps, "Vendor", "Tool.app"),
		filepath.Join(apps, "Vendor", "Deeper", "Deep.app"), filepath.Join(else_, "Other.app"),
		filepath.Join(apps, "Folder"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	desk := filepath.Join(root, "applications")
	if err := os.MkdirAll(filepath.Join(desk, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(desk, "editor.desktop"), filepath.Join(desk, "sub", "nested.desktop"),
		filepath.Join(else_, "planted.desktop"), filepath.Join(desk, "notes.txt")} {
		if err := os.WriteFile(f, []byte("[Desktop Entry]\nType=Application\nName=x\nExec=true\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(desk, "dir.desktop"), 0o755); err != nil {
		t.Fatal(err)
	}
	mac := []string{apps}
	for id, want := range map[string]bool{
		filepath.Join(apps, "Safari.app"):                                       true,
		filepath.Join(apps, "Vendor", "Tool.app"):                               true, // one folder below, as the list scans
		filepath.Join(apps, "Vendor", "Deeper", "Deep.app"):                     false,
		filepath.Join(else_, "Other.app"):                                       false,
		filepath.Join(apps, "Safari.app", "..", "..", "elsewhere", "Other.app"): false,
		filepath.Join(apps, "Folder"):                                           false, // not a bundle
		filepath.Join(apps, "Gone.app"):                                         false,
		"-a":                                                                    false,
		"Safari.app":                                                            false, // not a path in a folder the list scans
		"https://example.com/":                                                  false,
		"":                                                                      false,
	} {
		if got := darwinAppIDOK(id, mac); got != want {
			t.Errorf("macOS: %q allowed %v, want %v", id, got, want)
		}
	}
	lin := []string{desk}
	for id, want := range map[string]bool{
		filepath.Join(desk, "editor.desktop"):                     true,
		filepath.Join(desk, "sub", "nested.desktop"):              false, // the list reads each folder, not below it
		filepath.Join(else_, "planted.desktop"):                   false,
		filepath.Join(desk, "..", "elsewhere", "planted.desktop"): false,
		filepath.Join(desk, "notes.txt"):                          false,
		filepath.Join(desk, "dir.desktop"):                        false,
		filepath.Join(desk, "gone.desktop"):                       false,
		"-x.desktop":                                              false,
		"editor.desktop":                                          false,
	} {
		if got := linuxAppIDOK(id, lin); got != want {
			t.Errorf("Linux: %q allowed %v, want %v", id, got, want)
		}
	}
	// openApp itself refuses one outside, before it would run anything.
	if err := (linuxWindows{}).openApp(filepath.Join(else_, "planted.desktop")); err == nil {
		t.Error("Linux: an app outside the list's folders was opened")
	}
	if err := (darwinWindows{}).openApp(filepath.Join(else_, "Other.app")); err == nil {
		t.Error("macOS: an app outside the list's folders was opened")
	}
}
