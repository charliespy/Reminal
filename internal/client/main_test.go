// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"testing"
	"time"
)

// TestMain points HOME at a scratch directory for the whole package, so no
// test can reach the person's own ~/.reminal: since 3.15.11 anything that
// writes a record seals it, and a test binary minting the at-rest key in the
// real home is the key the person's daemon then adopts (2026-10-04). The
// atrest guard refuses such writes too; this keeps them from being tried.
func TestMain(m *testing.M) {
	// A process that only stands for a daemon (its args "reminal daemon"),
	// until it is killed: what daemon.pid names in a test.
	if os.Getenv("REMINAL_TEST_STAND_IN") != "" {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	// A test's machine in a process of its own (REMINAL_TEST_HOME: this
	// binary run by a test, as that machine) keeps the home it was given.
	if h := os.Getenv("REMINAL_TEST_HOME"); h != "" {
		os.Setenv("HOME", h)
		os.Setenv("REMINAL_KEYSTORE", "file")
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "reminal-test-home-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", dir)
	os.Setenv("USERPROFILE", dir)
	os.Setenv("REMINAL_KEYSTORE", "file")
	// The person's login shell is never asked: a test says what it holds.
	loginEnv = func() map[string]string { return nil }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
