// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A bundled helper (reminal-capture, reminal-overlay) gets our environment
// minus the dynamic loader's variables and reminal's own helper overrides; the
// lid dimmer's test stand-in only from a build without a version.
func TestFilterHelperEnv(t *testing.T) {
	env := []string{
		"HOME=/h", "PATH=/bin", "REMINAL_RELAY=ws://x",
		"DYLD_INSERT_LIBRARIES=/tmp/x.dylib", "DYLD_LIBRARY_PATH=/tmp", "LD_PRELOAD=/tmp/x.so",
		"REMINAL_CAPTURE_HELPER=/tmp/fake", "REMINAL_OVERLAY_BIN=/tmp/fake-overlay", "REMINAL_LIDDIM_FAKE=/tmp/f",
		"MY_DYLD_THING=kept", "LDFLAGS=-O2",
	}
	for _, tc := range []struct {
		release bool
		want    []string
	}{
		{true, []string{"HOME=/h", "PATH=/bin", "REMINAL_RELAY=ws://x", "MY_DYLD_THING=kept"}},
		{false, []string{"HOME=/h", "PATH=/bin", "REMINAL_RELAY=ws://x", "REMINAL_LIDDIM_FAKE=/tmp/f", "MY_DYLD_THING=kept"}},
	} {
		got := filterHelperEnv(env, tc.release)
		// LDFLAGS starts with "LD" but not "LD_": kept.
		want := append(tc.want, "LDFLAGS=-O2")
		if !slices.Equal(got, want) {
			t.Errorf("release=%v:\n got %q\nwant %q", tc.release, got, want)
		}
	}
}

// A release takes a helper override only from inside its own app bundle,
// symlinks resolved: a link inside the bundle pointing out of it is outside.
func TestInAppBundle(t *testing.T) {
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	macos := filepath.Join(root, "reminal.app", "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(macos, "reminal")
	inside := filepath.Join(macos, "reminal-capture")
	outside := filepath.Join(root, "reminal-capture")
	twin := filepath.Join(root, "reminal.app2", "reminal-capture") // shares the bundle's name as a prefix
	for _, p := range []string{exe, inside, outside, twin} {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	escape := filepath.Join(macos, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		p, exe string
		want   bool
	}{
		{inside, exe, true},
		{outside, exe, false},
		{twin, exe, false},
		{escape, exe, false},
		{inside, filepath.Join(root, "reminal"), false}, // a bare binary has no bundle
		{filepath.Join(macos, "missing"), exe, false},
	} {
		if got := inAppBundle(tc.p, tc.exe); got != tc.want {
			t.Errorf("inAppBundle(%s, %s) = %v, want %v", tc.p, tc.exe, got, tc.want)
		}
	}
}
