// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A settings file as a person keeps it: their own order and indentation,
// their own hooks, and reminal's.
const healSettings = `{
    "model": "opus",
    "hooks": {
        "Stop": [
            {"hooks": [{"type": "command", "command": "/usr/local/bin/reminal hook done"}], "reminalHook": true}
        ],
        "StopFailure": [
            {"hooks": [{"type": "command", "command": "notify-send 'claude stopped'"}]},
            {"hooks": [{"type": "command", "command": "/usr/local/bin/reminal hook failed"}], "reminalHook": true}
        ],
        "SessionStart": [{"hooks": [{"type": "command", "command": "/usr/local/bin/reminal hook ready"}]}]
    },
    "theme": "dark"
}
`

// Only reminal's own entry goes; every other byte stays — the person's
// entry for the event too. Theirs alone is never taken.
func TestOwnHookEntriesAreCutOutAndNothingElse(t *testing.T) {
	out, removed, err := removeOwnHookEntries([]byte(healSettings), []string{"hooks"}, "StopFailure")
	if err != nil || !removed {
		t.Fatalf("removed %v, %v", removed, err)
	}
	want := strings.Replace(healSettings, `,
            {"hooks": [{"type": "command", "command": "/usr/local/bin/reminal hook failed"}], "reminalHook": true}`, "", 1)
	if string(out) != want {
		t.Fatalf("got\n%s\nwant\n%s", out, want)
	}
	// Theirs alone: nothing to take.
	again, removed, err := removeOwnHookEntries(out, []string{"hooks"}, "StopFailure")
	if err != nil || removed || string(again) != string(out) {
		t.Fatalf("a person's own entry taken: removed %v, %v", removed, err)
	}
	// Ours alone (untagged, as an agent's rewrite leaves it): the key goes
	// with its list, its comma with it.
	only := `{"hooks": {"Stop": [], "StopFailure": [{"hooks": [{"type": "command", "command": "/usr/local/bin/reminal hook failed"}]}], "Notification": []}}`
	out, removed, err = removeOwnHookEntries([]byte(only), []string{"hooks"}, "StopFailure")
	if err != nil || !removed || string(out) != `{"hooks": {"Stop": [], "Notification": []}}` {
		t.Fatalf("ours alone: %q, %v, %v", out, removed, err)
	}
	last := `{"hooks": {"Stop": [], "StopFailure": [{"hooks": [{"type": "command", "command": "reminal hook failed"}]}]}}`
	if out, _, _ := removeOwnHookEntries([]byte(last), []string{"hooks"}, "StopFailure"); string(out) != `{"hooks": {"Stop": []}}` {
		t.Fatalf("ours the last key: %q", out)
	}
	for _, bad := range []string{`{"hooks": `, `not json`, `{"hooks": {"StopFailure": [}}`} {
		if out, removed, err := removeOwnHookEntries([]byte(bad), []string{"hooks"}, "StopFailure"); err == nil || removed || string(out) != bad {
			t.Errorf("%q: %q, %v, %v", bad, out, removed, err)
		}
	}
}

// Healed only for a Claude Code too old for the event: the file otherwise as
// it was, and said once. Newer, or its version not read, the file is not
// touched at all.
func TestHealTakesOutOnlyWhatTheAgentIsTooOldFor(t *testing.T) {
	for version, heal := range map[string]bool{
		"2.1.77 (Claude Code)":  true,
		"2.1.78 (Claude Code)":  false,
		"2.1.304 (Claude Code)": false,
		"":                      false,
	} {
		home := t.TempDir()
		path := filepath.Join(home, ".claude", "settings.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(healSettings), 0o640); err != nil {
			t.Fatal(err)
		}
		said := healHooks(home, func(bin string) string {
			if bin == "claude" {
				return version
			}
			return ""
		})
		got, _ := os.ReadFile(path)
		if !heal {
			if string(got) != healSettings || len(said) != 0 {
				t.Errorf("%q: touched (%q)", version, said)
			}
			continue
		}
		if strings.Contains(string(got), "reminal hook failed") || !strings.Contains(string(got), "notify-send 'claude stopped'") {
			t.Errorf("%q: %s", version, got)
		}
		if len(said) != 1 || !strings.HasPrefix(said[0], "reminal removed its StopFailure hook: claude 2.1.77 is older than 2.1.78. Start claude again to use reminal's hooks. A StopFailure hook of your own is still in ~/.claude/settings.json") {
			t.Errorf("%q: said %q", version, said)
		}
		if st, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o640) {
			t.Errorf("%q: its mode not kept: %v", version, err)
		}
		// Once: nothing more to take or say.
		if again := healHooks(home, func(string) string { return version }); len(again) != 0 {
			t.Errorf("%q: said again %q", version, again)
		}
	}
}

// --check: hooks set up for another version than the installed one are not
// set up — the Machines view offers to set them up again. Matching, they are.
func TestCheckSaysHooksForAnotherVersionAreNotSetUp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a stand-in claude on PATH is a shell script")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\necho stand-in\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	was := checkVersion
	t.Cleanup(func() { checkVersion = was })
	exe := "/usr/local/bin/reminal"
	for _, c := range []struct {
		written, installed string
		hooks              bool
	}{
		{"2.1.304", "2.1.304", true},
		{"2.1.77", "2.1.77", true},
		{"2.1.304", "2.1.77", false}, // taken back below it: StopFailure written, every hook lost
		{"2.1.77", "2.1.304", false}, // upgraded past it: StopFailure not written yet
		{"2.1.304", "", true},        // version not read: nothing said
	} {
		home := t.TempDir()
		spec := *targetNamed(t, "claude").hooks
		spec.version = c.written
		if err := applyHooks(&spec, home, exe, false); err != nil {
			t.Fatal(err)
		}
		installed := c.installed
		checkVersion = func(string) string { return installed }
		if st := checkIntegration(targetNamed(t, "claude"), home, exe); st.Hooks != c.hooks {
			t.Errorf("written for %s, %q installed: hooks %v, want %v", c.written, c.installed, st.Hooks, c.hooks)
		}
	}
}

const (
	healOurs   = `{"hooks": [{"type": "command", "command": "/usr/local/bin/reminal hook failed"}], "reminalHook": true}`
	healTheirs = `{"hooks": [{"type": "command", "command": "notify-send 'claude stopped %d'"}]}`
)

// healFile is a settings file whose StopFailure list holds entries in order
// (true: reminal's), laid out by sep between them and pad inside the list.
func healFile(order []bool, sep, pad string) string {
	var els []string
	for i, o := range order {
		if o {
			els = append(els, healOurs)
		} else {
			els = append(els, fmt.Sprintf(healTheirs, i))
		}
	}
	return `{"theme": "dark", "hooks": {"Stop": [], "StopFailure": [` + pad + strings.Join(els, sep) + pad + `], "Notification": []}, "z": 1}`
}

// Reminal's entries in any order among the person's: the cut leaves valid
// JSON with exactly theirs (review: two of ours before
// one of theirs cut theirs, and wrote a broken file).
func TestOwnHookEntriesInAnyOrder(t *testing.T) {
	for _, c := range []struct {
		order []bool
		want  string
	}{
		{[]bool{true, true, false}, `{"theme": "dark", "hooks": {"Stop": [], "StopFailure": [` + fmt.Sprintf(healTheirs, 2) + `], "Notification": []}, "z": 1}`},
		{[]bool{false, true, true}, `{"theme": "dark", "hooks": {"Stop": [], "StopFailure": [` + fmt.Sprintf(healTheirs, 0) + `], "Notification": []}, "z": 1}`},
		{[]bool{true, false, true}, `{"theme": "dark", "hooks": {"Stop": [], "StopFailure": [` + fmt.Sprintf(healTheirs, 1) + `], "Notification": []}, "z": 1}`},
		{[]bool{true, true}, `{"theme": "dark", "hooks": {"Stop": [], "Notification": []}, "z": 1}`},
	} {
		out, removed, err := removeOwnHookEntries([]byte(healFile(c.order, ", ", "")), []string{"hooks"}, "StopFailure")
		if err != nil || !removed || string(out) != c.want {
			t.Errorf("%v: %v %v\n got %s\nwant %s", c.order, removed, err, out, c.want)
		}
	}
	// More after the value: a file not read rightly — left as it is.
	bad := healFile([]bool{true, false}, ", ", "") + "\n}garbage"
	if out, removed, err := removeOwnHookEntries([]byte(bad), []string{"hooks"}, "StopFailure"); err == nil || removed || string(out) != bad {
		t.Errorf("trailing garbage: %q, %v, %v", out, removed, err)
	}
	trail := healFile([]bool{true, false}, ", ", "") + "\n\n"
	if _, removed, err := removeOwnHookEntries([]byte(trail), []string{"hooks"}, "StopFailure"); err != nil || !removed {
		t.Errorf("whitespace after the value: %v, %v", removed, err)
	}
}

// Every order of up to six entries, laid out three ways: the result is valid
// JSON and reads as the original with exactly reminal's entries gone.
func TestOwnHookEntriesEveryOrderKeepsTheRest(t *testing.T) {
	layouts := [][2]string{{", ", ""}, {",\n      ", "\n      "}, {",", " "}}
	for n := 1; n <= 6; n++ {
		for bits := 0; bits < 1<<n; bits++ {
			order := make([]bool, n)
			for i := range order {
				order[i] = bits&(1<<i) != 0
			}
			for _, l := range layouts {
				raw := []byte(healFile(order, l[0], l[1]))
				out, removed, err := removeOwnHookEntries(raw, []string{"hooks"}, "StopFailure")
				if err != nil {
					t.Fatalf("%v %q: %v", order, l, err)
				}
				if removed != (bits != 0) {
					t.Fatalf("%v %q: removed %v", order, l, removed)
				}
				if !json.Valid(out) || !healedAsExpected(raw, out, []string{"hooks"}, "StopFailure") {
					t.Fatalf("%v %q: %s", order, l, out)
				}
			}
		}
	}
}

// Fuzzed: whatever the file, the cut is either nothing (the file as it was)
// or valid JSON reading as the original with exactly reminal's entries gone.
func FuzzRemoveOwnHookEntries(f *testing.F) {
	f.Add([]byte(healSettings))
	f.Add([]byte(healFile([]bool{true, true, false}, ", ", "")))
	f.Add([]byte(healFile([]bool{false, true, false, true}, ",\n", "\n")))
	f.Fuzz(func(t *testing.T, raw []byte) {
		out, removed, err := removeOwnHookEntries(raw, []string{"hooks"}, "StopFailure")
		if !removed {
			if string(out) != string(raw) {
				t.Fatalf("not removed, and changed: %v", err)
			}
			return
		}
		if !json.Valid(out) || !healedAsExpected(raw, out, []string{"hooks"}, "StopFailure") {
			t.Fatalf("cut to %q from %q", out, raw)
		}
	})
}

// A settings file that is a link (kept in a dotfiles repo) stays a link: its
// target is healed, its mode kept. A link to nothing is refused.
func TestHealWritesWhatALinkNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := t.TempDir()
	dots := filepath.Join(home, "dotfiles", "claude-settings.json")
	if err := os.MkdirAll(filepath.Dir(dots), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dots, []byte(healSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dots, link); err != nil {
		t.Fatal(err)
	}
	removed, err := removeOwnHookFile(link, []string{"hooks"}, "StopFailure")
	if err != nil || !removed {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
	got, _ := os.ReadFile(dots)
	if strings.Contains(string(got), "reminal hook failed") || !strings.Contains(string(got), "notify-send") {
		t.Fatalf("its target: %s", got)
	}
	if st, _ := os.Stat(dots); st.Mode().Perm() != 0o644 {
		t.Fatalf("its target's mode: %v", st.Mode())
	}
	dangling := filepath.Join(home, "dangling.json")
	if err := os.Symlink(filepath.Join(home, "nowhere.json"), dangling); err != nil {
		t.Fatal(err)
	}
	if removed, err := removeOwnHookFile(dangling, []string{"hooks"}, "StopFailure"); err == nil || removed {
		t.Fatalf("a link to nothing: %v, %v", removed, err)
	}
}

// Changed between the heal's read and its write (claude, or a person,
// writing it): nothing is written over theirs.
func TestHealWritesNothingOverAChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(healSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	theirs := strings.Replace(healSettings, `"theme": "dark"`, `"theme": "light"`, 1)
	was := healBeforeWrite
	healBeforeWrite = func(target string) { _ = os.WriteFile(target, []byte(theirs), 0o600) }
	t.Cleanup(func() { healBeforeWrite = was })
	if removed, err := removeOwnHookFile(path, []string{"hooks"}, "StopFailure"); err != errHealRaced || removed {
		t.Fatalf("removed %v, %v", removed, err)
	}
	if got, _ := os.ReadFile(path); string(got) != theirs {
		t.Fatalf("written over a change: %s", got)
	}
}

// --heal-hooks heals only for the version the session said its own agent
// is: with none said, reminal's own PATH is never asked, and nothing done.
func TestHealOnlyForTheVersionTheSessionSaid(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(healSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	if said := healHooks(home, sessionVersions(map[string]string{})); len(said) != 0 {
		t.Fatalf("healed with no version said: %q", said)
	}
	if got, _ := os.ReadFile(path); string(got) != healSettings {
		t.Fatal("touched with no version said")
	}
	if said := healHooks(home, sessionVersions(map[string]string{"claude": "2.1.77 (Claude Code)"})); len(said) != 1 {
		t.Fatalf("not healed for the version said: %q", said)
	}
}

// --check asks a CLI its version once for each binary: kept by its path,
// size and time.
func TestCheckAsksAVersionOncePerBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a stand-in claude on PATH is a shell script")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := t.TempDir()
	count := filepath.Join(bin, "asked")
	script := "#!/bin/sh\necho x >> " + count + "\necho '2.1.77 (Claude Code)'\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	for i := 0; i < 3; i++ {
		if v := installedVersion("claude"); v != "2.1.77 (Claude Code)" {
			t.Fatalf("version %q", v)
		}
	}
	if b, _ := os.ReadFile(count); strings.Count(string(b), "x") != 1 {
		t.Fatalf("asked %d times", strings.Count(string(b), "x"))
	}
}

// integrate itself writes what a linked settings file names: the link stays.
func TestIntegrateKeepsALinkedSettingsFileALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := t.TempDir()
	dots := filepath.Join(home, "dotfiles", "claude.json")
	if err := os.MkdirAll(filepath.Dir(dots), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dots, []byte(`{"theme": "dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dots, link); err != nil {
		t.Fatal(err)
	}
	spec := *targetNamed(t, "claude").hooks
	spec.version = "2.1.304"
	if err := applyHooks(&spec, home, "/usr/local/bin/reminal", false); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("integrate replaced the link with a file")
	}
	if got, _ := os.ReadFile(dots); !strings.Contains(string(got), "reminal hook done") || !strings.Contains(string(got), `"theme": "dark"`) {
		t.Fatalf("its target: %s", got)
	}
	if st, _ := os.Stat(dots); st.Mode().Perm() != 0o644 {
		t.Fatalf("its target's mode changed to %v", st.Mode().Perm())
	}
}
