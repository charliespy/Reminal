// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"reminal/internal/session"
)

// agentEnvNames are the variables that say where a coding agent keeps its
// files, and where it is found.
var agentEnvNames = []string{"CLAUDE_CONFIG_DIR", "PI_CODING_AGENT_DIR", "PATH"}

// loginEnv is the person's login shell's values of agentEnvNames, asked
// once per process (session.LoginEnv); a test stands in for it.
var loginEnv = sync.OnceValue(func() map[string]string { return session.LoginEnv(agentEnvNames...) })

// agentEnv is variable name as the agents a person starts see it: this
// process's when set here, else their login shell's. The daemon, and the
// sessions it starts (a viewer's wake), have neither their rc's variables
// nor its PATH, so a check made there would look where the agent does not.
func agentEnv(name string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return loginEnv()[name]
}

// agentLookPath finds program bin as the person's shell would: on their
// login PATH first, then on this process's.
func agentLookPath(bin string) (string, error) {
	for _, dir := range filepath.SplitList(loginEnv()["PATH"]) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, bin)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return exec.LookPath(bin)
}

// dirEnv is the folder variable name says, else the default under home.
func dirEnv(name string, home ...string) string {
	if dir := strings.TrimSpace(agentEnv(name)); dir != "" {
		return dir
	}
	return homeUnder(home...)
}
