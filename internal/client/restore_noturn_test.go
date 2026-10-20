// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"reminal/internal/session"
)

// An agent sent nothing yet has no conversation to resume: an agent writes
// one on its first message, and Claude Code's --resume of one never written
// is "No conversation found" — the session was left a bare shell, where
// whatever was typed next ran as a command (a machine that went down
// before its agent's first turn). Started afresh instead, its flags kept,
// whatever the agent.
func TestAnAgentSentNothingIsStartedAfreshNotResumed(t *testing.T) {
	useHelp(t)
	cases := []struct {
		name string
		r    session.Restore
		want []string
	}{
		{"claude, its id known, keeps its flags and drops its prompt",
			session.Restore{Fg: "claude", FgArgs: []string{"node", "/opt/claude/cli.js", "--model", "opus", "--dangerously-skip-permissions", "fix the tests"}, Conv: "a1b2c3d4-e5f6", NoTurn: true},
			[]string{"claude", "--model", "opus", "--dangerously-skip-permissions"}},
		{"claude with no id: not its latest either",
			session.Restore{Fg: "claude", FgArgs: []string{"claude"}, NoTurn: true},
			[]string{"claude"}},
		{"codex by id",
			session.Restore{Fg: "codex", FgArgs: []string{"codex"}, Conv: "0199aaaa-bbbb", NoTurn: true},
			[]string{"codex"}},
		{"cursor-agent keeps -f",
			session.Restore{Fg: "cursor-agent", FgArgs: []string{"cursor-agent", "-f"}, NoTurn: true},
			[]string{"cursor-agent", "-f"}},
		{"gemini, latest only",
			session.Restore{Fg: "gemini", FgArgs: []string{"node", "/usr/local/bin/gemini"}, NoTurn: true},
			[]string{"gemini"}},
		{"pi keeps a provider given as a flag",
			session.Restore{Fg: "pi", FgArgs: []string{"pi", "--provider", "fake", "-e", "/x/p.ts"}, NoTurn: true},
			[]string{"pi", "--provider", "fake", "-e", "/x/p.ts"}},
		{"print mode is still never started",
			session.Restore{Fg: "claude", FgArgs: []string{"claude", "-p", "hi"}, NoTurn: true}, nil},
	}
	for _, c := range cases {
		if got := resumeArgv(c.r); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	// Two in one folder, neither sent anything: each started afresh, no
	// list opened to pick a conversation that is not there.
	a := session.Restore{ID: "AAAA0001", Fg: "claude", FgArgs: []string{"claude"}, Cwd: "/w", NoTurn: true}
	b := session.Restore{ID: "BBBB0001", Fg: "claude", FgArgs: []string{"claude"}, Cwd: "/w", NoTurn: true}
	if argv, note := resumePlan(a, []session.Restore{b}); !reflect.DeepEqual(argv, []string{"claude"}) || note != "" {
		t.Errorf("shared folder, nothing sent: got %q, note %q", argv, note)
	}
	// A record from before NoTurn: resumed as before.
	if got := resumeArgv(session.Restore{Fg: "claude", FgArgs: []string{"claude"}, Conv: "a1b2c3d4-e5f6"}); !reflect.DeepEqual(got, []string{"claude", "--resume", "a1b2c3d4-e5f6"}) {
		t.Errorf("an older record changed: %q", got)
	}
}

// What says a conversation had a turn: one begun since the agent came to
// the front, by its state or its hook, or the record of the same agent and
// conversation already saying so (a resumed one ran its turns before).
func TestATurnIsSeenByStateHookOrTheRecordBefore(t *testing.T) {
	isolateHome(t)
	front := time.Now().Add(-time.Minute)
	r := session.Restore{ID: "TURN0001", Fg: "claude", Conv: "conv-aaaa"}
	a := &Agent{sessionID: "TURN0001", attnFGAt: front}
	if a.turnSeenIn(r) {
		t.Fatal("an agent sent nothing was taken to have run a turn")
	}
	// A turn before it came to the front (another agent's, or its last
	// run's) is not this one's.
	a.attnTurnAt = front.Add(-time.Second)
	if a.turnSeenIn(r) {
		t.Fatal("a turn from before the agent came to the front was counted")
	}
	a.attnTurnAt = front.Add(time.Second)
	if !a.turnSeenIn(r) {
		t.Fatal("a turn its state showed was not counted")
	}
	b := &Agent{sessionID: "TURN0001", attnFGAt: time.Now().Add(-time.Second)}
	time.Sleep(10 * time.Millisecond)
	if err := session.WriteHookState("TURN0001", "working"); err != nil {
		t.Fatal(err)
	}
	if !b.turnSeenIn(r) {
		t.Fatal("a turn its hook said began was not counted")
	}
	// Resumed: the record before says its conversation had a turn.
	c := &Agent{sessionID: "TURN0002", attnFGAt: time.Now()}
	if err := session.WriteRestore(session.Restore{ID: "TURN0002", Fg: "claude", Conv: "conv-bbbb", SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if !c.turnSeenIn(session.Restore{ID: "TURN0002", Fg: "claude", Conv: "conv-bbbb"}) {
		t.Fatal("a resumed conversation's earlier turns were forgotten: it would start afresh")
	}
	// Another conversation in its place (a /clear): not carried.
	if c.turnSeenIn(session.Restore{ID: "TURN0002", Fg: "claude", Conv: "conv-cccc"}) {
		t.Fatal("a new conversation took the old one's turns")
	}
	// Started into a conversation it has (claude --continue: the latest
	// here, someone's with turns): written, though it sent nothing itself.
	d := &Agent{sessionID: "TURN0003", attnFGAt: time.Now()}
	if !d.turnSeenIn(session.Restore{ID: "TURN0003", Fg: "claude", FgArgs: []string{"claude", "--continue"}, Conv: "conv-dddd"}) {
		t.Fatal("an agent started into its conversation (--continue) was taken to have none")
	}
	if !d.turnSeenIn(session.Restore{ID: "TURN0003", Fg: "codex", FgArgs: []string{"codex", "resume", "--last"}}) {
		t.Fatal("codex resume was taken to have no conversation")
	}
	if d.turnSeenIn(session.Restore{ID: "TURN0003", Fg: "claude", FgArgs: []string{"claude", "--", "--continue is just words here"}}) {
		t.Fatal("a prompt's words were read as a flag")
	}
	// A record saying none: none carried.
	if err := session.WriteRestore(session.Restore{ID: "TURN0002", Fg: "claude", Conv: "conv-bbbb", NoTurn: true, SavedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if c.turnSeenIn(session.Restore{ID: "TURN0002", Fg: "claude", Conv: "conv-bbbb"}) {
		t.Fatal("a record saying no turn was read as one")
	}
}

// By its hook, by conversation: one the agent went back into (/resume, with
// nothing sent yet) is written, though no turn ran since it came to the
// front; and once its hook names its conversations, a turn it ran in an
// earlier one (before a /clear) does not make a new one written.
func TestAConversationIsWrittenByItsOwnHooksWord(t *testing.T) {
	isolateHome(t)
	a := &Agent{sessionID: "CONV0001", attnFGAt: time.Now()}
	if err := session.WriteConv("CONV0001", "conv-picked"); err != nil {
		t.Fatal(err)
	}
	if err := session.MarkConvWritten("CONV0001", "conv-picked"); err != nil {
		t.Fatal(err)
	}
	if !a.turnSeenIn(session.Restore{ID: "CONV0001", Fg: "claude", FgArgs: []string{"claude"}, Conv: "conv-picked"}) {
		t.Fatal("a conversation picked with /resume, nothing sent, was taken to be unwritten: restore would leave it")
	}
	// After a /clear: a new conversation, a turn before it.
	b := &Agent{sessionID: "CONV0002", attnFGAt: time.Now().Add(-time.Minute), attnTurnAt: time.Now().Add(-30 * time.Second)}
	if err := session.WriteConv("CONV0002", "conv-cleared"); err != nil {
		t.Fatal(err)
	}
	if b.turnSeenIn(session.Restore{ID: "CONV0002", Fg: "claude", FgArgs: []string{"claude"}, Conv: "conv-cleared"}) {
		t.Fatal("a turn from before a /clear made the new, unwritten conversation resumable")
	}
	// Started afresh, it says how to go back by hand.
	argv, note := resumePlan(session.Restore{ID: "CONV0002", Fg: "claude", FgArgs: []string{"claude", "--model", "opus"}, Conv: "a1b2c3d4-e5f6", NoTurn: true}, nil)
	if !reflect.DeepEqual(argv, []string{"claude", "--model", "opus"}) || !strings.Contains(note, "--resume a1b2c3d4-e5f6") {
		t.Fatalf("got %q, note %q: want a fresh start and its conversation named", argv, note)
	}
}
