// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// Gemini CLI's first screen in a folder it has not seen asks whether to trust
// it — a choice only the person can make, so the session needs them: never
// "done" (nothing to do), and never "logged out" (it is not a login). As
// Gemini CLI 0.62.0 draws it, 80 and 120 columns wide.
func TestGeminisTrustDialogNeedsYou(t *testing.T) {
	for _, w := range []string{"80", "120"} {
		raw, err := os.ReadFile(filepath.Join("testdata", "harness-screens", "gemini-trust-"+w+".txt"))
		if err != nil {
			t.Fatal(err)
		}
		tail := attentionProbeTail(string(raw), attnTailRows)
		if got := classifyAttn(true, tail, 5000); got != "input" {
			t.Errorf("%s columns: %q, want input (needs you)", w, got)
		}
		if harnessProblemIn(harnessTail(string(raw), harnessRows), "gemini") {
			t.Errorf("%s columns: read as logged out", w)
		}
	}
}
