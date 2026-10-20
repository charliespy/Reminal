// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package pty

import (
	"strings"
	"testing"

	"reminal/internal/session"
)

// A probe's mark never enters a session's shell: a session started from
// anything carrying it is a real one, whose agent's hooks must write.
func TestAProbesMarkNeverEntersASessionsShell(t *testing.T) {
	if hookProbeEnv != session.HookProbeEnv {
		t.Fatalf("the mark dropped here is %q, the one set is %q", hookProbeEnv, session.HookProbeEnv)
	}
	t.Setenv(session.HookProbeEnv, "1")
	for _, kv := range shellEnv([]string{"REMINAL_SESSION=AAAA2345"}) {
		if strings.HasPrefix(kv, session.HookProbeEnv+"=") {
			t.Fatalf("a session's shell carries the probe's mark: %q", kv)
		}
	}
}
