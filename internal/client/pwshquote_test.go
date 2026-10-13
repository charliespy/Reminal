// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/json"
	"os"
	"testing"
)

// pwshQuoteValues are values PowerShell must read back as themselves from
// what shellCommand types: each kind of single quote it ends a string at.
var pwshQuoteValues = []string{
	"x’; New-Item -Path /tmp/q1 -ItemType File | Out-Null; ’y",
	"x‘; New-Item -Path /tmp/q2 -ItemType File | Out-Null; ‘y",
	"x‚; New-Item -Path /tmp/q3 -ItemType File | Out-Null; ‚y",
	"x‛; New-Item -Path /tmp/q4 -ItemType File | Out-Null; ‛y",
	"x'; New-Item -Path /tmp/q5 -ItemType File | Out-Null; 'y",
	"plain value with spaces",
}

// What shellCommand types on PowerShell, for scripts/restore-test/pwsh-quote.sh
// to have a real PowerShell read back (REMINAL_PWSH_QUOTES names the file).
func TestPowerShellQuotingForTheRig(t *testing.T) {
	out := os.Getenv("REMINAL_PWSH_QUOTES")
	if out == "" {
		t.Skip("run by scripts/restore-test/pwsh-quote.sh")
	}
	type line struct{ Value, Typed string }
	var lines []line
	for _, v := range pwshQuoteValues {
		lines = append(lines, line{v, shellCommand([]string{"Write-Output", v}, "pwsh")})
	}
	b, _ := json.Marshal(lines)
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
