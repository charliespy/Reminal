// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"regexp"
	"testing"
)

// The viewer sends resume right behind pake_init and relies on the agent
// writing the handshake's answer — the session key — before the screen the
// resume asks for. That holds while the agent's reader answers pake_init
// itself, before it reads the next frame. Moving it onto a goroutine (as the
// owner handshake is) would let the screen overtake the key and leave a
// switched-to session blank, so the dispatch is pinned here.
func TestPakeInitIsAnsweredOnTheReader(t *testing.T) {
	src, err := os.ReadFile("agent.go")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)case protocol\.TypePakeInit:\s*(?://[^\n]*\n\s*)*(go\s+)?a\.handlePakeInit\(`).FindSubmatch(src)
	if m == nil {
		t.Fatal("the TypePakeInit dispatch is no longer where this test looks for it — fix the test, do not delete it")
	}
	if len(m[1]) > 0 {
		t.Fatal("pake_init is answered on a goroutine: the screen a resume asks for can then reach the viewer before its key")
	}
}
