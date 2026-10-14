// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"

	"reminal/internal/session"
)

// The bottom of Claude Code's screen after a message reached it logged out,
// as it was seen on a phone (2026-09-18).
const loggedOutScreen = `● Login expired · Please run /login

✻ Churned for 0s · done 7:33 AM

────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────
  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents          Not logged in · Run /login
                                                        ✔ Update installed · Restart to update`

// An agent at work on login code, saying so in its answer just above the
// prompt — which must not read as the agent itself being logged out.
const proseScreen = `● The bug was that the session check ran before the cookie was set, so a
  user who had just signed up was not logged in on the first page load, and
  the redirect to /login looped. Fixed in auth/session.go.

────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────
  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents`

// The same screen as Claude Code actually leaves it — spaces drawn as
// cursor moves, so the words run together (read from vm-two, 2026-09-18).
const loggedOutScreenGlued = `●Loginexpired·Pleaserun/login

✻Brewedfor0s·done7:34AM

──────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────
⏵⏵automodeon (shift+tabtocycle)·←foragentsNotloggedin·Run/login
✔Updateinstalled·Restarttoupdate`

func TestHarnessLoggedOutIsSeenWithoutSpaces(t *testing.T) {
	if !harnessProblemIn(attentionProbeTail(loggedOutScreenGlued, harnessRows), "claude") {
		t.Fatal("a logged-out Claude Code screen, as rendered, was not recognised")
	}
	footer := "────\n❯\n────\n⏵⏵automodeon (shift+tabtocycle)·←foragentsNotloggedin·Run/login"
	if !harnessProblemIn(footer, "claude") {
		t.Fatal("the status line alone, as rendered, was not recognised")
	}
	gluedProse := "●Thebugwasthatauserwhohadjustsignedupwasnotloggedinonthefirstpageload,andthe\n redirectto/loginlooped.Fixedinauth/session.go."
	if harnessProblemIn(gluedProse, "claude") {
		t.Fatal("an agent's prose about logins, run together, was read as a lapsed login")
	}
}

func TestHarnessLoggedOutIsSeenAndProseIsNot(t *testing.T) {
	if !harnessProblemIn(attentionProbeTail(loggedOutScreen, harnessRows), "claude") {
		t.Fatal("a logged-out Claude Code screen was not recognised")
	}
	footerOnly := "────\n❯\n────\n  ⏵⏵ auto mode on (shift+tab to cycle)          Not logged in · Run /login"
	if !harnessProblemIn(footerOnly, "claude") {
		t.Fatal("the status line's \"Not logged in\" alone was not recognised")
	}
	if harnessProblemIn(attentionProbeTail(proseScreen, harnessRows), "claude") {
		t.Fatal("an agent's own words about logins were read as its login lapsing")
	}
}

// Seen twice in a row it counts; it must be gone for a while to clear.
func TestHarnessHealthDebounces(t *testing.T) {
	a := &Agent{}
	a.noteHarnessHealth(loggedOutScreen, "claude", false)
	if a.harnessDown() {
		t.Fatal("down after one glimpse")
	}
	a.noteHarnessHealth(loggedOutScreen, "claude", false)
	if !a.harnessDown() {
		t.Fatal("not down after two")
	}
	for i := 0; i < harnessUpAfter-1; i++ {
		a.noteHarnessHealth(proseScreen, "claude", false)
	}
	if !a.harnessDown() {
		t.Fatal("cleared too soon")
	}
	a.noteHarnessHealth(proseScreen, "claude", false)
	if a.harnessDown() {
		t.Fatal("did not clear once the login came back")
	}
}

// End to end through the terminal emulator the probe reads: Claude Code's
// bytes — styled words, spaces as cursor moves — rendered, then judged.
// (Hand-written screens hid that the render carries its styling.)
func TestHarnessLoggedOutThroughTheEmulator(t *testing.T) {
	e := vt.NewEmulator(100, 10)
	_, _ = e.Write([]byte("\x1b[38;5;174m\u25cf\x1b[39m \x1b[1mLogin\x1b[1Cexpired\x1b[0m\x1b[1C\u00b7\x1b[1CPlease\x1b[1Crun\x1b[1C/login\r\n\r\n"))
	_, _ = e.Write([]byte("\x1b[2m\u23f5\u23f5 auto mode on\x1b[0m\x1b[30C\x1b[31mNot\x1b[1Clogged\x1b[1Cin\x1b[1C\u00b7\x1b[1CRun\x1b[1C/login\x1b[0m"))
	a := &Agent{}
	a.noteHarnessHealth(e.Render(), "claude", false)
	a.noteHarnessHealth(e.Render(), "claude", false)
	if !a.harnessDown() {
		t.Fatalf("not seen through the emulator; render was %q", e.Render())
	}
}

// Other CLIs start on a sign-in screen when they have no login. These are the
// bottoms of their real screens.
func TestHarnessSignInScreensAreLoggedOut(t *testing.T) {
	screens := map[string]string{
		"agy": " Welcome to the Antigravity CLI. You are currently not signed in.\n Select login method:\n" +
			" > 1. Google OAuth\n   2. Use a Google Cloud project\n   ↑/↓ Navigate · enter Select",
		// As they are drawn: a dialog is mostly its own blank rows.
		"gemini": "│   How would you like to authenticate for this project?                       │\n" +
			"│                                                                              │\n" +
			"│   No authentication method selected.                                         │\n" +
			"│                                                                              │\n" +
			"│   (Use Enter to select)                                                      │\n" +
			"│                                                                              │\n" +
			"│   Terms of Services and Privacy Notice for Gemini CLI                        │\n" +
			"│                                                                              │\n" +
			"│   https://geminicli.com/docs/resources/tos-privacy/                          │\n" +
			"╰──────────────────────────────────────────────────────────────────────────────╯\n" +
			"│   ● 1. Sign in with Google                                                   │\n" +
			"│     2. Use Gemini API Key                                                    │\n" +
			"│   (Use Enter to select)                                                      │",
		"amp":                "No API key found. Starting login flow...\nWould you like to log in to Amp? [(y)es, (n)o]: ",
		"codex":              "  Sign in with ChatGPT to use Codex as part of your paid plan\n  › 1. Sign in with ChatGPT\n    2. Provide your own API key",
		"codex, turned away": "■ unexpected status 401 Unauthorized: Incorrect API key provided:\nkey at https://platform.openai.com/account/api-keys.",
		"qwen": "  │ Alibaba ModelStudio · Access Method                                      │\n" +
			"  │                                                                          │\n" +
			"  │ › Coding Plan                                                            │\n" +
			"  │   For individual developers · Weekly quota included                      │\n" +
			"  │                                                                          │\n" +
			"  │   Token Plan                                                             │\n" +
			"  │                                                                          │\n" +
			"  │   Standard API Key                                                       │\n" +
			"  │   Connect with an existing ModelStudio API key                           │\n" +
			"  │                                                                          │\n" +
			"  │ Enter to select, ↑↓ to navigate, Esc to go back                          │\n" +
			"  └──────────────────────────────────────────────────────────────────────────┘",
	}
	for name, s := range screens {
		if prog, _, _ := strings.Cut(name, ","); !harnessProblemIn(harnessTail(s, harnessRows), prog) {
			t.Errorf("%s's sign-in screen was not seen as logged out", name)
		}
	}
	for _, ok := range []string{
		"  › Ask Codex to do anything   ? for shortcuts",
		" Type your message or @path/to/file",
		"I added a sign-in page; the user signs in with Google.",
	} {
		if harnessProblemIn(ok, "") {
			t.Errorf("read as logged out: %q", ok)
		}
	}
}

// A lead relaying what it saw on a report's screen quotes the notice word
// for word, mid-sentence. That is not the lead being logged out — read as one,
// it marked a working lead logged out and held its mail (seen on the demo org,
// 2026-09-24; both screens below are as they were).
func TestAQuotedLoginNoticeIsNotTheAgents(t *testing.T) {
	lead := `● This confirms it: frontend2's Claude Code session shows "Login expired · Please run /login" — it
  needs an actual person to complete a login/auth flow, which isn't something I can do on its behalf.
  Needs your attention: frontend2 (n_8eaa75, on ubuntu-linux-22-04-02-desktop, ~/design) is logged out
  of Claude Code. It's sitting at a prompt asking for /login. Since that requires re-authenticating
  with a real account/credential, I can't push that through myself — could you log into that terminal?
  Work queued for frontend2 will stay held until then.
✻ Baked for 11s · done 7:08 AM
──────────────────────────────────────────────────────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents
                                                              ✔ Update installed · Restart to update`
	if harnessProblemIn(attentionProbeTail(lead, harnessRows), "claude") {
		t.Fatal("a lead quoting its report's login notice was read as the lead logged out")
	}
	// A quote that wraps so the notice begins a row is still a quote.
	wrapped := "● This confirms it: frontend2's session shows\n  \"Login expired · Please run /login\" — it needs a person.\n" +
		"────────────────\n❯\n────────────────\n  ⏵⏵ auto mode on (shift+tab to cycle)"
	if harnessProblemIn(attentionProbeTail(wrapped, harnessRows), "claude") {
		t.Fatal("a wrapped quote of the notice was read as the agent logged out")
	}

	report := `● Login expired · Please run /login
✻ Churned for 0s · done 7:08 AM
──────────────────────────────────────────────────────────────────────────────────────────────────────
❯
──────────────────────────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle) · ← for agents
                                                                          Not logged in · Run /login
                                                              ✔ Update installed · Restart to update`
	if !harnessProblemIn(attentionProbeTail(report, harnessRows), "claude") {
		t.Fatal("the report that really is logged out was not seen as one")
	}
	// Either place alone is enough: the conversation line, or the status line.
	if !harnessProblemIn("● Login expired · Please run /login\n❯", "claude") {
		t.Fatal("Claude Code's own notice on a line of its own was missed")
	}
	if !harnessProblemIn("❯\n  ⏵⏵ bypass permissions on (shift+tab to cycle)          Not logged in · Run /login", "claude") {
		t.Fatal("the status line's notice was missed")
	}
}

// Another program's login, printed by one of the agent's tools, is not the
// agent's. Such a line sits in the conversation above the prompt; a CLI's own
// notice sits on its bottom rows.
func TestAToolsLoginMessageIsNotTheAgents(t *testing.T) {
	working := `● Checking the deploy.

● Bash(netlify status)
  ⎿  Not logged in. Please log in to see site status.

● I'll ask you to log in to Netlify, then check again.

────────────────────────────────────────────────────
❯
────────────────────────────────────────────────────
  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents`
	if harnessProblemIn(attentionProbeTail(working, harnessRows), "claude") {
		t.Fatal("a tool's output about another service's login was read as the agent logged out")
	}
	// A CLI saying it of itself, on its own bottom rows, still counts.
	for _, own := range []string{
		"some output\nmore output\n> \n Waiting for auth… (esc to cancel)",
		"answer\n\nPlease log in to continue\n> ",
	} {
		if !harnessProblemIn(attentionProbeTail(own, harnessRows), "claude") {
			t.Fatalf("a CLI's own login notice was missed:\n%s", own)
		}
	}
}

// A Claude Code session's screen as it was when it was read three times as
// logged out (2026-10-14): at work, its tool having printed Codex's sign-in
// menu (a check of Codex's login). Codex's sign-in screen is Codex's; this is
// Claude Code, logged in. (Only its paths are changed.)
const anotherAgentsSignInScreen = `  ⎿  Shell cwd was reset to /home/dev/app

⏺ Bash(cd /home/dev/app/scripts/logins-probe && cat > run.sh
      <<'EOF'
      #!/bin/sh…)
  ⎿  SigninwithChatGPT
        sent:
     == codex exec, OPENAI_API_KEY
        sent:
     +18 lines (5m 34s · timeout 10m)
     (ctrl+b to run in background)

✻ Mustering… (38m 52s · ↓ 34.0k tokens)
  ⎿  Tip: Use /clear to start fresh when switching topics and free up context

──────────────────────────────────────────────────────────────────────────────────────────────────────────
❯ 
──────────────────────────────────────────────────────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle) · esc to interrupt · ← 2 agents`

func TestAnotherAgentsSignInScreenIsNotThisAgents(t *testing.T) {
	if harnessProblemIn(harnessTail(anotherAgentsSignInScreen, harnessRows), "claude") {
		t.Fatal("Codex's sign-in menu, printed by a tool, read as Claude Code logged out")
	}
	a := &Agent{}
	for i := 0; i < 5; i++ {
		a.noteHarnessHealth(anotherAgentsSignInScreen, "claude", false)
	}
	if a.harnessDown() {
		t.Fatal("the session was marked logged out")
	}
	// Codex's own sign-in screen, with Codex running, still is.
	codex := "  Sign in with ChatGPT to use Codex as part of your paid plan\n  › 1. Sign in with ChatGPT\n    2. Provide your own API key"
	if !harnessProblemIn(harnessTail(codex, harnessRows), "codex") {
		t.Fatal("Codex's own sign-in screen was missed")
	}
	// A program the probe does not name as an agent ("node": an agent run
	// from a path it does not know) is not known: every sign-in screen counts.
	gemini := "│   How would you like to authenticate for this project?   │\n│   ● 1. Sign in with Google   │"
	if !harnessProblemIn(harnessTail(gemini, harnessRows), "node") {
		t.Fatal("Gemini's sign-in screen under \"node\" was missed")
	}
	// And Claude Code's notice is Claude Code's alone: a Codex session's tool
	// printing it is not Codex logged out.
	if harnessProblemIn(harnessTail(loggedOutScreen, harnessRows), "codex") {
		t.Fatal("Claude Code's notice read as Codex logged out")
	}
}

// A turn its hook says has been going for a while, its screen still moving,
// is an agent that can work: what the screen says of logins does not make it
// logged out, and a session marked so is cleared at once.
func TestASessionSeenAtWorkIsNotLoggedOut(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		hs      *session.HookState
		settled int64
		want    bool
		what    string
	}{
		{&session.HookState{State: "working", TS: now.Add(-38 * time.Minute)}, 300, false, "past the hook's TTL"},
		{&session.HookState{State: "working", TS: now.Add(-5 * time.Minute)}, 300, true, "a long turn, its spinner moving"},
		{&session.HookState{State: "working", TS: now.Add(-2 * time.Second)}, 300, false, "a turn just begun (a logged-out one ends at once)"},
		{&session.HookState{State: "working", TS: now.Add(-5 * time.Minute)}, 5000, false, "a still screen"},
		{&session.HookState{State: "done", TS: now.Add(-5 * time.Minute)}, 300, false, "a finished turn"},
		{nil, 300, false, "no hook"},
	} {
		if got := harnessAtWork(c.hs, now, c.settled); got != c.want {
			t.Errorf("%s: at work %v, want %v", c.what, got, c.want)
		}
	}
	a := &Agent{}
	a.noteHarnessHealth(loggedOutScreen, "claude", false)
	a.noteHarnessHealth(loggedOutScreen, "claude", false)
	if !a.harnessDown() {
		t.Fatal("not down")
	}
	a.noteHarnessHealth(loggedOutScreen, "claude", true)
	if a.harnessDown() {
		t.Fatal("seen at work, still logged out")
	}
}
