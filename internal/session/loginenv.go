// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// loginEnvWait is how long the login shell is given to answer.
const loginEnvWait = 4 * time.Second

// envNameRe is what LoginEnv asks for: a variable's name, never anything a
// shell would run.
var envNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoginEnv asks the user's login shell for the variables names, as it sets
// them for what it starts: for a process that was not started from it (the
// daemon, a session the daemon started), which has neither its rc's
// variables nor its PATH. A name it leaves unset is left out; nil when the
// shell can't be asked (Windows, no SHELL, no answer in time).
//
// Each value is read between sentinels: an rc file that prints a banner
// would otherwise run into it. The shell runs as a probe (ProbeCommand): in
// a session of its own with a short wait on its pipes, so an rc that starts
// something long-lived cannot hold the question open, and apart from any
// session, so an agent an rc starts writes nothing of one.
func LoginEnv(names ...string) map[string]string {
	shell := os.Getenv("SHELL")
	if runtime.GOOS == "windows" || shell == "" {
		return nil
	}
	const mark = "__REMINAL_ENV__"
	var script strings.Builder
	for _, n := range names {
		if envNameRe.MatchString(n) {
			script.WriteString(`[ -n "${` + n + `+x}" ] && printf '\n` + mark + `%s=%s` + mark + `' ` + n + ` "$` + n + `"; `)
		}
	}
	script.WriteString("true")
	ctx, cancel := context.WithTimeout(context.Background(), loginEnvWait)
	defer cancel()
	out, err := RunProbe(ProbeCommand(ctx, shell, "-l", "-c", script.String()), false)
	if err != nil {
		return nil
	}
	env := map[string]string{}
	parts := strings.Split(string(out), mark)
	for i := 1; i < len(parts); i += 2 {
		if k, v, ok := strings.Cut(parts[i], "="); ok {
			env[k] = strings.TrimSpace(v)
		}
	}
	return env
}
