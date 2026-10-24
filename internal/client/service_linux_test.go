// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"testing"
)

// fakeManager puts systemctl and loginctl on PATH: systemctl exits with code
// (0: as a user manager that took the unit; else: none to ask, as in a
// container), and on a successful enable makes the link enable makes.
func fakeManager(t *testing.T, home string, code int) {
	t.Helper()
	bin := t.TempDir()
	wants := filepath.Join(home, ".config", "systemd", "user", "default.target.wants")
	sc := "#!/bin/sh\n" +
		"[ " + strconv.Itoa(code) + " = 0 ] || { echo 'Failed to connect to bus: No medium found' >&2; exit " + strconv.Itoa(code) + "; }\n" +
		"case \" $* \" in *' enable '*) mkdir -p '" + wants + "' && ln -sf ../" + daemonUnit + " '" + wants + "/" + daemonUnit + "' ;; esac\n"
	// As root, systemctl is asked through `sudo -u <user> env …` (userSystemctl).
	sudo := "#!/bin/sh\nshift 2\nexec \"$@\"\n"
	for name, body := range map[string]string{"systemctl": sc, "loginctl": "#!/bin/sh\nexit 0\n", "sudo": sudo} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func homeUser(t *testing.T) *user.User {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	u := *me
	u.HomeDir = t.TempDir()
	return &u
}

// A machine with no user manager (a container, a minimal server): enabling
// the unit fails, and it is not left behind looking installed — so its
// sessions serve its machine channel themselves, rather than wait on a daemon
// nothing will ever run (an upgrade after which the machine never came back).
func TestADaemonUnitNoManagerTookIsNotLeftLookingInstalled(t *testing.T) {
	u := homeUser(t)
	fakeManager(t, u.HomeDir, 1)
	if err := installService("/opt/reminal/reminal", u); err == nil {
		t.Fatal("installed with no user manager to enable it")
	}
	if serviceInstalled(u) {
		t.Error("a unit no manager took counts as installed")
	}
	if _, err := os.Stat(filepath.Join(u.HomeDir, ".config", "systemd", "user", daemonUnit)); !os.IsNotExist(err) {
		t.Errorf("the unit file was left behind: %v", err)
	}
}

// One a user manager enabled is installed.
func TestADaemonUnitTheManagerEnabledIsInstalled(t *testing.T) {
	u := homeUser(t)
	fakeManager(t, u.HomeDir, 0)
	if err := installService("/opt/reminal/reminal", u); err != nil {
		t.Fatal(err)
	}
	if !serviceInstalled(u) {
		t.Error("an enabled unit does not count as installed")
	}
}

// A machine an older version left with the unit file and no link (its
// enable failed, the file kept): not installed, with nothing to clean up
// first.
func TestAUnitFileAloneIsNotAnInstalledService(t *testing.T) {
	u := homeUser(t)
	dir := filepath.Join(u.HomeDir, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, daemonUnit), []byte(systemdUnit("/opt/reminal/reminal")), 0o644); err != nil {
		t.Fatal(err)
	}
	if serviceInstalled(u) {
		t.Error("a unit file never enabled counts as installed")
	}
	if err := os.MkdirAll(filepath.Join(dir, "default.target.wants"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../"+daemonUnit, filepath.Join(dir, "default.target.wants", daemonUnit)); err != nil {
		t.Fatal(err)
	}
	if !serviceInstalled(u) {
		t.Error("enabled, it does not count as installed")
	}
}
