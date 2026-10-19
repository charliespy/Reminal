// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"testing"
	"time"
)

// What each harness shows at its limit, and where that came from. None of
// these could be driven to a real limit (that needs an account at it), so
// each is its CLI's own template, from the installed release's bundle or
// binary (Docker, 2026-10-19), filled in as the CLI fills it:
//
//	claude        2.1.303   source  "You've hit your <name> limit · resets <time> (<zone>)"
//	codex         0.163.0   source  "You’ve hit your usage limit. … try again at <time>."
//	gemini        0.64.0    source  "Usage limit reached for <model>." / "Access resets at <time> <zone>."
//	qwen          0.25.3    source  "[API Error: … (Status: 429)]" / "Possible quota limitations …"
//	opencode      1.19.3    source  "Usage limit reached. It will reset in <h> hours <m> minutes. …"
//	goose         1.55.0    source  "Rate limit exceeded: …", "Credits exhausted: …"
//	crush         0.98.1    inferred: no notice of its own; the service's words under "Provider Error"
//	cursor-agent  2026.10.18 source "Upgrade your plan to continue" (the limit's words are its service's)
//
// The logged-out screens (TestHarnessLoggedOutScreens) were each captured
// from the CLI itself, run with no login.
var limitScreens = []struct {
	prog, screen string
	resets       time.Time // zero: says none
}{
	{"claude", "  ⎿  You've hit your session limit · resets 3pm (America/Los_Angeles)\n✻ Churned for 0s\n────\n❯ \n────\n  ⏵⏵ auto mode on", limitLA(2026, 10, 19, 15, 0)},
	{"claude", "  ⎿  You’ve hit your weekly limit · resets Oct 21, 3:30pm (America/Los_Angeles) · progress saved\n❯ ", limitLA(2026, 10, 21, 15, 30)},
	{"claude", "  ⎿  You've hit your usage limit · resets 9am (America/Los_Angeles)\n     /upgrade to increase your usage limit.\n❯ ", limitLA(2026, 10, 20, 9, 0)},
	{"claude", "● You've hit your limit · resets 11:15am (America/Los_Angeles)\n❯ ", limitLA(2026, 10, 19, 11, 15)},
	{"claude", "  ⎿  You're out of usage credits · resets 5pm (America/Los_Angeles)\n❯ ", limitLA(2026, 10, 19, 17, 0)},
	{"claude", "  ⎿  Usage limit reached · limit resets 4pm\n❯ ", limitLA(2026, 10, 19, 16, 0)},
	{"claude", "  ⎿  You've hit your org's monthly spend limit\n❯ ", time.Time{}},
	{"codex", "■ You’ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit\nhttps://chatgpt.com/settings/usage to purchase more credits or try again at 3:05 PM.\n\n› ", limitLA(2026, 10, 19, 15, 5)},
	{"codex", "■ You've hit your usage limit. Visit https://chatgpt.com/settings/usage to purchase more credits or try again at Oct 21st, 2026 3:05 PM.\n› ", limitLA(2026, 10, 21, 15, 5)},
	{"codex", "■ You’ve hit your usage limit. To get more access now, send a request to your admin or try again later.\n› ", time.Time{}},
	{"gemini", "✕ Usage limit reached for gemini-2.5-pro.\n  Access resets at 3:05 PM PDT.\n  /stats model for usage details\n  /model to switch models.\n╭───╮\n│ > Type your message │", limitLA(2026, 10, 19, 15, 5)},
	{"gemini", "✕ You have exhausted your daily quota on this model.\n│ > │", time.Time{}},
	{"qwen", "✕ [API Error: Request failed with status code 429 (Status: 429)]\nPossible quota limitations in place or slow response times detected. Please wait and try again later.\n│ > │", time.Time{}},
	{"opencode", "┃  Usage limit reached. It will reset in 3 hours 20 minutes. To continue using this model now, enable usage from your available balance\n┃", limitLA(2026, 10, 19, 13, 20)},
	{"opencode", "┃  Rate limit exceeded, retry in 45s\n┃", limitLA(2026, 10, 19, 10, 0).Add(45 * time.Second)},
	{"goose", "  Ran into this error: Rate limit exceeded: 429 Too Many Requests.\n\n  Please retry if you think this is a transient or recoverable error.\n( O)> ", time.Time{}},
	{"goose", "  Rate limited — wait a moment and try again.\n( O)> ", time.Time{}},
	{"crush", "  Provider Error\n  You exceeded your current quota, please check your plan and billing details.\n", time.Time{}},
	{"cursor-agent", "  ⚠ Usage limit\n  Upgrade your plan to continue\n  → ", time.Time{}},
}

var limitTestNow = limitLA(2026, 10, 19, 10, 0)

func limitLA(y int, mo time.Month, d, h, m int) time.Time {
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		return time.Time{}
	}
	return time.Date(y, mo, d, h, m, 0, 0, la)
}

func TestHarnessLimitScreens(t *testing.T) {
	if limitTestNow.IsZero() {
		t.Skip("no zone data")
	}
	for _, tc := range limitScreens {
		hit, at := harnessLimitIn(tc.screen, tc.prog, limitTestNow)
		if !hit {
			t.Errorf("%s: limit not seen in %q", tc.prog, tc.screen)
			continue
		}
		if !at.Equal(tc.resets) {
			t.Errorf("%s: resets %v, want %v (%q)", tc.prog, at, tc.resets, tc.screen)
		}
		// An agent run from a path the probe does not know: still seen.
		if hit, _ := harnessLimitIn(tc.screen, "", limitTestNow); !hit {
			t.Errorf("%s: not seen with the program unknown", tc.prog)
		}
	}
}

// Never a person's own text, an agent talking about limits, or one CLI's
// words on another's screen.
func TestHarnessLimitIsNotConversation(t *testing.T) {
	for _, tc := range []struct{ prog, screen, why string }{
		{"claude", "❯ You've hit your session limit · resets 3pm (America/Los_Angeles)", "typed at the prompt"},
		{"claude", "> You've hit your session limit · resets 3pm", "a person's message, echoed"},
		{"claude", "│ > You've hit your usage limit · resets 3pm │", "in the input box"},
		{"claude", "● The report said \"You've hit your session limit · resets 3pm\", so I waited.", "quoted in an answer"},
		{"claude", "● When a seat shows Usage limit reached, reminal reports it as limited.", "discussed mid-sentence"},
		{"claude", "  ⎿  You've used 90% of your session limit · resets 3pm (America/Los_Angeles)", "a warning, not the limit"},
		{"claude", "  ⎿  Approaching session limit · resets 3pm", "a warning, not the limit"},
		{"codex", "› You’ve hit your usage limit. Try again at 3:05 PM.", "typed at Codex's prompt"},
		{"codex", "• Codex prints “You’ve hit your usage limit” when the plan runs out.", "an answer about it"},
		{"gemini", "│ > Usage limit reached for gemini-2.5-pro. │", "typed at Gemini's prompt"},
		{"goose", "( O)> Rate limit exceeded: what does that mean?", "typed at goose's prompt"},
		{"goose", "  The API answers 429 when the rate limit is exceeded: back off and retry.", "an answer about it"},
		{"opencode", "┃  Add backoff so a usage limit reached error retries later.", "an answer about it"},
		// Another CLI's words, where this one runs: a Codex session whose tool
		// printed Claude Code's notice is not at its limit.
		{"codex", "  ⎿  You've hit your session limit · resets 3pm (America/Los_Angeles)", "Claude Code's notice under Codex"},
		{"claude", "✕ Usage limit reached for gemini-2.5-pro.", "Gemini CLI's notice under Claude Code"},
		// A plain shell: nothing of an agent's here, and no notice that is.
		{"bash", "$ echo \"You've hit your session limit\"", "a shell command"},
	} {
		if hit, _ := harnessLimitIn(tc.screen, tc.prog, limitTestNow); hit {
			t.Errorf("%s (%s): read as at its limit: %q", tc.prog, tc.why, tc.screen)
		}
	}
}

// Seen twice to count, its reset time kept, and over the moment the agent is
// at work again, or the notice has gone.
func TestHarnessLimitState(t *testing.T) {
	if limitTestNow.IsZero() {
		t.Skip("no zone data")
	}
	now := limitTestNow
	defer func() { harnessNow = time.Now }()
	harnessNow = func() time.Time { return now }
	screen := limitScreens[0].screen
	a := &Agent{}
	if a.noteHarnessLimit(screen, "claude", false, time.Time{}) {
		t.Fatal("one glimpse counted")
	}
	if !a.noteHarnessLimit(screen, "claude", false, time.Time{}) {
		t.Fatal("seen twice, not limited")
	}
	if l, at := a.harnessLimitedNow(); !l || !at.Equal(limitLA(2026, 10, 19, 15, 0)) {
		t.Fatalf("limited=%v resets %v", l, at)
	}
	if a.activeRecord(0).Attn != "" {
		t.Fatal("the record's state is the probe's to set")
	}
	a.attnState = harnessLimited
	if r := a.activeRecord(0); !r.ResetsAt.Equal(limitLA(2026, 10, 19, 15, 0)) {
		t.Fatalf("record resets_at %v", r.ResetsAt)
	}
	// At work: over at once.
	if !a.noteHarnessLimit(screen, "claude", true, time.Time{}) {
		t.Fatal("at work and still limited")
	}
	if l, at := a.harnessLimitedNow(); l || !at.IsZero() {
		t.Fatalf("after work: limited=%v resets %v", l, at)
	}
	// The notice it had is from before it worked: not news.
	a.noteHarnessLimit(screen, "claude", false, time.Time{})
	a.noteHarnessLimit(screen, "claude", false, time.Time{})
	if l, _ := a.harnessLimitedNow(); l {
		t.Fatal("the notice from before it worked read as the limit again")
	}
	// A new one under it: limited again. Then both scroll away: over after
	// harnessUpFor.
	a.noteHarnessLimit(screen+"\n"+screen, "claude", false, time.Time{})
	a.noteHarnessLimit(screen+"\n"+screen, "claude", false, time.Time{})
	a.noteHarnessLimit("❯ ", "claude", false, time.Time{})
	if l, _ := a.harnessLimitedNow(); !l {
		t.Fatal("one read without it cleared it")
	}
	now = now.Add(harnessUpFor)
	if !a.noteHarnessLimit("❯ ", "claude", false, time.Time{}) {
		t.Fatal("gone for harnessUpFor, still limited")
	}
}

// A turn begun after the limit was found ends it, though its notice is still
// in the conversation above; the harness turning that turn away too adds a
// notice of its own, and that is the limit again.
func TestHarnessLimitEndsWithATurn(t *testing.T) {
	if limitTestNow.IsZero() {
		t.Skip("no zone data")
	}
	now := limitTestNow
	defer func() { harnessNow = time.Now }()
	harnessNow = func() time.Time { return now }
	notice := "  ⎿  You've hit your session limit · resets 3pm (America/Los_Angeles)"
	a := &Agent{}
	turn := now.Add(-time.Minute) // the turn the harness turned away
	a.noteHarnessLimit(notice+"\n❯ ", "claude", false, turn)
	a.noteHarnessLimit(notice+"\n❯ ", "claude", false, turn)
	if l, _ := a.harnessLimitedNow(); !l {
		t.Fatal("not limited")
	}
	// After the reset, "continue": a turn begins, and it is answered.
	now = now.Add(5 * time.Hour)
	turn = now
	answered := notice + "\n❯ continue\n● Picking up where I left off.\n❯ "
	if !a.noteHarnessLimit(answered, "claude", false, turn) {
		t.Fatal("a turn began and it is still limited")
	}
	for i := 0; i < 3; i++ {
		now = now.Add(time.Second)
		a.noteHarnessLimit(answered, "claude", false, turn)
	}
	if l, _ := a.harnessLimitedNow(); l {
		t.Fatal("the old notice, still on screen, read as the limit again")
	}
	// Turned away again: a second notice.
	turn = now
	again := answered + "\n❯ go on\n" + notice + "\n❯ "
	now = now.Add(time.Second)
	a.noteHarnessLimit(again, "claude", false, turn)
	now = now.Add(time.Second)
	a.noteHarnessLimit(again, "claude", false, turn)
	if l, _ := a.harnessLimitedNow(); !l {
		t.Fatal("a new notice under the old one not read as the limit")
	}
}

// The login screens of the CLIs the table lacked, as each showed them.
func TestHarnessLoggedOutScreens(t *testing.T) {
	for _, tc := range []struct{ prog, screen string }{
		// goose 1.55, no provider (captured).
		{"goose", "•  Telemetry disabled. You can enable it anytime in settings.\n  error: No provider configured. Run 'goose configure' first."},
		// Crush 0.98, first run (captured).
		{"crush", "  To start, let's choose a provider and model.\n  > Find your fave\n  ↑/↓ choose • enter confirm"},
		{"crush", "ERROR\nNo providers configured - please run 'crush' to set up a provider interactively."},
		// Cursor CLI, `cursor-agent -p` with no login (captured).
		{"cursor-agent", "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable."},
		// Qwen Code, `qwen -p` with no auth (captured).
		{"qwen", "No auth type is selected. Please configure an auth type (e.g. via settings or `--auth-type`) before running in non-interactive mode."},
		// OpenCode with a provider and no key (source).
		{"opencode", "┃  Anthropic API key is missing. Pass it using the 'apiKey' parameter or the ANTHROPIC_API_KEY environment variable."},
		// Those already in the table, as captured again on 2026-10-19.
		{"claude", "❯ hi\n  ⎿  Not logged in · Please run /login\n✻ Churned for 0s · done 7:38 PM\n────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)                    Not logged in · Run /login"},
		{"codex", "  Welcome to Codex, OpenAI's command-line coding agent\n  Sign in with ChatGPT to use Codex as part of your paid plan\n> 1. Sign in with ChatGPT"},
		{"gemini", "│ ? Get started │\n│   How would you like to authenticate for this project? │\n│   ● 1. Sign in with Google │"},
		{"qwen", "  │ Connect a Provider │\n  │ › QwenCloud │"},
		{"cursor-agent", "        Cursor Agent\n        v2026.10.18-ea085cb\n        Press any key to log in..."},
	} {
		if !harnessProblemIn(tc.screen, tc.prog) {
			t.Errorf("%s: logged out not seen in %q", tc.prog, tc.screen)
		}
	}
	// And a goose that is set up, talking about configuring: not logged out.
	if harnessProblemIn("  To use another model, run goose configure and pick a provider.\n( O)> ", "goose") {
		t.Error("goose's advice read as its login screen")
	}
	if harnessProblemIn(strings.Repeat("x", 3), "crush") {
		t.Error("nothing read as crush logged out")
	}
}
