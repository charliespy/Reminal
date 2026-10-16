// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The viewer turns text in the terminal into links it will open. Only
// http(s) is a link: anything else a program prints is text.
func TestTerminalLinksAreHTTPOnly(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	s := viewerText(t, "web")
	start := strings.Index(s, "const reminalLinks = (function () {")
	if start < 0 {
		t.Fatal("reminalLinks is no longer where this test looks for it — fix the test, do not delete it")
	}
	end := strings.Index(s[start:], "\n    })();\n")
	if end < 0 {
		t.Fatal("reminalLinks' end is no longer where this test looks for it")
	}
	block := s[start : start+end+len("\n    })();\n")]
	js := block + `
const rows = (t) => [{ text: t, wrapped: false }];
const found = (t) => reminalLinks.linksInRows(rows(t), 0).map(k => k.url);
const out = {};
for (const t of [
  'run javascript:alert(1) now', 'see data:text/html,<b>x</b>', 'vbscript:MsgBox(1)',
  'JaVaScRiPt:alert(1)', '   javascript:alert(1)', 'file:///etc/passwd',
  '\u001b]8;;javascript:alert(1)\u0007click\u001b]8;;\u0007', 'ftp://example.com/x',
]) out[t] = found(t);
out['https://example.com/a'] = found('open https://example.com/a now');
out['http://example.com'] = found('http://example.com');
process.stdout.write(JSON.stringify(out));
`
	// A cap on node, so one that hangs fails this test, not the package's
	// whole run at its time limit.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "-e", js)
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	t0 := time.Now()
	err = cmd.Run()
	took := time.Since(t0)
	// It has failed only on GitHub's runners, never in Docker: once hung for
	// minutes, once "exit status 1" after 5 s with nothing printed. What it
	// says when it fails is all there is to go on, so it says all of it.
	if ctx.Err() != nil || err != nil {
		t.Fatalf("node failed: %s\n%s", nodeFailure(ctx, cmd, err, took), nodeContext(node, &stdout, &stderr))
	}
	var res map[string][]string
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		t.Fatalf("node output: %v\n%s", err, nodeContext(node, &stdout, &stderr))
	}
	for text, links := range res {
		switch text {
		case "https://example.com/a", "http://example.com":
			if len(links) != 1 || links[0] != text {
				t.Errorf("%q: found %v, want itself", text, links)
			}
		default:
			if len(links) != 0 {
				t.Errorf("%q was offered as a link: %v", text, links)
			}
		}
	}
}

// nodeFailure says how a node run ended: its exit code or the signal that
// ended it, whether it was stopped at the cap, whether its output was held
// open past WaitDelay (by a process that inherited it), how long it took
// against the CPU it used.
func nodeFailure(ctx context.Context, cmd *exec.Cmd, err error, took time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v after %s", err, took.Round(time.Millisecond))
	if ctx.Err() != nil {
		b.WriteString("; stopped at the 30 s cap")
	}
	if errors.Is(err, exec.ErrWaitDelay) || (cmd.ProcessState != nil && cmd.WaitDelay > 0 && took >= cmd.WaitDelay && ctx.Err() == nil) {
		b.WriteString("; it ended, but its output stayed open until WaitDelay gave up: another process held it")
	}
	if ps := cmd.ProcessState; ps != nil {
		fmt.Fprintf(&b, "; exit code %d", ps.ExitCode())
		if ws, ok := ps.Sys().(interface {
			Signaled() bool
			Signal() os.Signal
		}); ok && ws.Signaled() {
			fmt.Fprintf(&b, ", ended by %v", ws.Signal())
		}
		fmt.Fprintf(&b, "; CPU user %s, system %s", ps.UserTime().Round(time.Millisecond), ps.SystemTime().Round(time.Millisecond))
	} else {
		b.WriteString("; it never started")
	}
	return b.String()
}

// nodeContext is what was around it: what it printed, each stream apart,
// which node it was, and the other processes this test binary has running.
func nodeContext(node string, stdout, stderr *bytes.Buffer) string {
	var b strings.Builder
	fmt.Fprintf(&b, "stdout (%d bytes): %q\nstderr (%d bytes): %q\n", stdout.Len(), clip(stdout.String()), stderr.Len(), clip(stderr.String()))
	// Listed first: running node again below would change what is there.
	if kids := testProcesses(); kids != "" {
		fmt.Fprintf(&b, "this test binary's children, and the rest of its process group:\n%s", kids)
	}
	real, _ := filepath.EvalSymlinks(node)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vc := exec.CommandContext(ctx, node, "--version")
	vc.WaitDelay = time.Second
	v, verr := vc.CombinedOutput()
	fmt.Fprintf(&b, "node: %s (%s), --version: %q %v\n", node, real, strings.TrimSpace(string(v)), verr)
	return b.String()
}

func clip(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "…"
	}
	return s
}

// testProcesses lists, from /proc ("" where there is none), the processes
// whose parent is this test binary or that are in its process group (a
// child's child is reparented away, but keeps the group): one an earlier
// test, or node itself, left running is where an inherited pipe or a stray
// signal would come from.
func testProcesses() string {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	me, grp := fmt.Sprint(os.Getpid()), fmt.Sprint(syscallGetpgrp())
	// pid → [state, ppid, pgrp], from each /proc/<pid>/stat (the fields after
	// the last ')', the name before it may hold anything).
	procs := map[string][]string{}
	for _, e := range ents {
		if e.Name() == "" || strings.Trim(e.Name(), "0123456789") != "" {
			continue
		}
		st, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		s := string(st)
		if i := strings.LastIndexByte(s, ')'); i >= 0 {
			if f := strings.Fields(s[i+1:]); len(f) >= 3 {
				procs[e.Name()] = f[:3]
			}
		}
	}
	// This binary and what started it (go test, a shell) are not news.
	mine := map[string]bool{}
	for p := me; p != "" && p != "0" && !mine[p]; {
		mine[p] = true
		if f := procs[p]; f != nil {
			p = f[1]
		} else {
			break
		}
	}
	var b strings.Builder
	for pid, f := range procs {
		if mine[pid] || (f[1] != me && f[2] != grp) {
			continue
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
		fmt.Fprintf(&b, "  pid %s state %s ppid %s pgrp %s: %s\n", pid, f[0], f[1], f[2], clip(strings.ReplaceAll(strings.TrimRight(string(cmdline), "\x00"), "\x00", " ")))
	}
	return b.String()
}

func TestIsNotReady(t *testing.T) {
	if isNotReady(nil) {
		t.Fatal("nil error is not 'not ready'")
	}
	for _, m := range []string{"session not ready", "session not found or not ready", "auth: session not ready"} {
		if !isNotReady(errors.New(m)) {
			t.Errorf("%q should be temporary", m)
		}
	}
	if isNotReady(errors.New("handshake failed: PIN mismatch")) {
		t.Fatal("a PIN mismatch is not temporary")
	}
}
