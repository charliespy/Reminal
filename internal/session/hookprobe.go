// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package session

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"reminal/internal/procgroup"
)

// HookProbeEnv marks a program reminal runs only to ask it something (an
// agent's --version): set in its environment, and in everything it starts.
//
// Such a program is a child of the session's own reminal, and so inside the
// session by the process tree (Enclosing). One that does not know the
// question starts up instead — a wrapper, a shim, a test's stand-in — and its
// hooks fire: a SessionStart naming a conversation of its own was recorded
// as the session's, and a restore resumed that conversation, or none. Under
// the mark, `reminal hook` writes nothing, and Enclosing names no session.
const HookProbeEnv = "REMINAL_HOOK_PROBE"

// ProbeEnv is environ for a probe: every REMINAL_ variable left out (the
// session it would name is not its own) and the mark set.
func ProbeEnv(environ []string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "REMINAL_") {
			out = append(out, kv)
		}
	}
	return append(out, HookProbeEnv+"=1")
}

// ProbeCommand is a probe of name with args: its environment ProbeEnv's, no
// input (it reads none: os/exec's null device), ended with ctx, in a session
// of its own whose group a timeout kills whole (procgroup.BoundSession). Run
// it with RunProbe, which records that session while it runs.
func ProbeCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = ProbeEnv(os.Environ())
	procgroup.BoundSession(cmd)
	return cmd
}

// RunProbe runs cmd (ProbeCommand's) to its end and is what it wrote to its
// output — its errors too when combined — as cmd.Output or CombinedOutput.
//
// While it runs, its session is recorded (~/.reminal/probes): the mark in
// its environment is gone from a program that clears its own (`exec env
// -i`, a harness that starts its hooks with none), but the session it was
// started in stays its descendants', and Enclosing refuses an ancestor in a
// recorded one. Recorded once it has started: a hook fired in the moment
// before is held by the mark alone.
func RunProbe(cmd *exec.Cmd, combined bool) ([]byte, error) {
	var out bytes.Buffer
	cmd.Stdout = &out
	if combined {
		cmd.Stderr = &out
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	forget := recordProbe(cmd.Process.Pid)
	err := cmd.Wait()
	forget()
	return out.Bytes(), err
}

// probesDir is where running probes' sessions are recorded: a file each,
// named for the session (its leader's pid), holding its leader's start
// (probeIdent) so a pid used again names no probe.
func probesDir() (string, error) {
	d, err := activeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "probes"), nil
}

// recordProbe records the session led by pid (a probe's: ProbeCommand starts
// it in its own) and is what lets it go. Written first, then the records a
// reminal left dying mid-probe are let go — their leader gone or another
// process — so the sweep never widens the moment before it is recorded;
// and a "<pid>.tmp" left by one that died between writing and renaming,
// once no process has that pid.
func recordProbe(pid int) (forget func()) {
	ident, ok := probeIdent(pid)
	dir, err := probesDir()
	if !ok || err != nil || os.MkdirAll(dir, 0o700) != nil {
		return func() {}
	}
	path := filepath.Join(dir, strconv.Itoa(pid))
	tmp := path + ".tmp"
	if os.WriteFile(tmp, []byte(ident), 0o600) != nil || os.Rename(tmp, path) != nil {
		_ = os.Remove(tmp)
		return func() {}
	}
	sweepProbeRecords(dir)
	return func() { _ = os.Remove(path) }
}

// sweepProbeRecords lets go of dir's records that are no running probe's,
// and of the half-written ones ("<pid>.tmp") whose pid no process has.
func sweepProbeRecords(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if base, ok := strings.CutSuffix(name, ".tmp"); ok {
			if n, err := strconv.Atoi(base); err == nil {
				if _, alive := probeIdent(n); !alive {
					_ = os.Remove(filepath.Join(dir, name))
				}
			}
			continue
		}
		if n, err := strconv.Atoi(name); err == nil && !probeRecordLive(dir, n) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// probeRecordLive says whether dir's record for session sid is a running
// probe's: its leader the process that started then.
func probeRecordLive(dir string, sid int) bool {
	b, err := os.ReadFile(filepath.Join(dir, strconv.Itoa(sid)))
	if err != nil {
		return false
	}
	ident, ok := probeIdent(sid)
	return ok && string(b) == ident
}

// probeSessions is the sessions of running probes, as recorded; nil when
// there are none (Enclosing then reads no process's session).
func probeSessions() (dir string, sids map[int]bool) {
	dir, err := probesDir()
	if err != nil {
		return "", nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil
	}
	for _, e := range entries {
		if n, err := strconv.Atoi(e.Name()); err == nil && n > 0 {
			if sids == nil {
				sids = map[int]bool{}
			}
			sids[n] = true
		}
	}
	return dir, sids
}

// InProbe says whether this process runs under a probe's mark.
func InProbe() bool { return os.Getenv(HookProbeEnv) != "" }

// probeMarked says whether pid was started under a probe's mark: what a
// process started from it carries even where it scrubs its own environment
// (a harness's hooks and helpers may be started so). False where it cannot
// be read.
func probeMarked(pid int) bool {
	for _, kv := range processEnv(pid) {
		if strings.HasPrefix(kv, HookProbeEnv+"=") && kv != HookProbeEnv+"=" {
			return true
		}
	}
	return false
}
