// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"strings"
	"testing"
)

// loginRowCase is a row a CLI draws when it cannot run for want of a login,
// as it draws it: prog's own, a sign-in screen's or a notice's (or, refused,
// a key it was given and turned away).
type loginRowCase struct {
	prog, row string
	refused   bool // a key it was given and refused (harnessKeyRefusals)
	bottom    bool // read only on the last few rows: its status line
	notice    bool // Claude Code's notice shape (harnessNoticeRow), read on its own row only
}

var loginRowCases = []loginRowCase{
	{prog: "agy", row: " Welcome to the Antigravity CLI. You are currently not signed in."},
	{prog: "agy", row: " You are currently not signed in."},
	{prog: "agy", row: " Sign in required", bottom: true},

	{prog: "codex", row: "  Sign in with ChatGPT to use Codex as part of your paid plan"},
	{prog: "codex", row: "› 1. Sign in with ChatGPT"},
	{prog: "codex", row: "  3. Provide your own API key"},
	{prog: "codex", row: "■ Your access token could not be refreshed because your refresh token has expired. Please log out and sign in again."},
	{prog: "codex", row: "■ Your access token could not be refreshed because your refresh token was revoked. Please log out and sign in again."},
	{prog: "codex", row: "■ Your access token could not be refreshed because you have since logged out or signed in to another account. Please sign in again."},
	{prog: "codex", row: "■ Your session has expired. Please reauthenticate."},

	{prog: "amp", row: "Would you like to log in to Amp? [(y)es, (n)o]: "},
	{prog: "amp", row: "No API key found. Starting login flow..."},
	{prog: "amp", row: "No API key found. Run `amp login` first."},
	{prog: "amp", row: "Authentication failed. Run `amp login` and try again."},
	{prog: "amp", row: "API key required. Please run `amp login` first."},
	{prog: "amp", row: "API key required for headless mode. Please run `amp login` first."},
	{prog: "amp", row: "Session expired and could not be refreshed. Run 'amp login' to authenticate."},
	{prog: "amp", row: "Your session has expired. You need to log in again."},
	{prog: "amp", row: "Access token expired. Run 'amp login' to refresh."},
	{prog: "amp", row: "Invalid or missing API key. Run 'amp login' to authenticate."},

	{prog: "claude", row: " ✢ Opening browser to sign in…"},
	{prog: "claude", row: " Browser didn't open? Use the url below to sign in (c to copy)"},
	{prog: "claude", row: "● OAuth token revoked · Please run /login", notice: true},
	{prog: "claude", row: "● Login expired · Run /login to sign in again, or re-authenticate your Anthropic profile"},
	{prog: "claude", row: "● Not signed in to Claude. Run /login first."},
	{prog: "claude", row: "  ⎿  Failed to authenticate: OAuth token revoked. Please log in again or contact your administrator."},
	{prog: "claude", row: "  ⎿  Failed to authenticate: OAuth session expired and could not be refreshed"},
	{prog: "claude", row: "● Invalid API key · Fix external API key", refused: true},

	{prog: "gemini", row: "✕ Gemini API key is missing or not configured."},

	{prog: "qwen", row: "  │   Connect with an existing ModelStudio API key                           │"},
	{prog: "qwen", row: "✕ No API key found: set OPENAI_API_KEY or security.auth.apiKey."},
	{prog: "qwen", row: "✕ Qwen OAuth credentials expired. Please use /auth to re-authenticate with qwen-oauth."},
	{prog: "qwen", row: "✕ Refresh token expired or invalid. Please use '/auth' to re-authenticate."},
	{prog: "qwen", row: `✕ [API Error: Invalid API key. Coding Plan API keys start with "sk-sp-". Please check. (Status: 401)]`, refused: true},
	{prog: "qwen", row: `✕ Invalid API key. Pay-as-you-go keys start with "sk-" or "sk-ws-". Token Plan keys ("sk-sp-") need the Token Plan entry instead.`, refused: true},
	{prog: "qwen", row: "✕ Model API authentication failed (HTTP 401). Check the configured provider credentials.", refused: true},

	{prog: "opencode", row: "  ┃  Anthropic API key is missing. Pass it using the 'apiKey' parameter or the ANTHROPIC_API_KEY environment variable."},
	{prog: "opencode", row: "  ┃  Unauthorized: request was blocked by a gateway or proxy. Your authentication token may be missing or expired — try running `opencode auth login <your provider URL>` to re-authenticate."},

	{prog: "goose", row: "Authentication failed — check your API key. Run `goose configure` to update it."},
	{prog: "goose", row: "Muse Code API key is missing. Sign in again."},
	{prog: "goose", row: "Meta session expired. Sign in to Muse Code again."},

	{prog: "crush", row: `Please sign in to Hyper again. You can also run "crush login hyper --force".`},
	{prog: "crush", row: "You are not logged in to any platform."},

	{prog: "cursor-agent", row: "                                               Press any key to log in..."},
	{prog: "cursor-agent", row: " If your browser didn't open, click this link to log in:"},
	{prog: "cursor-agent", row: " Press q to show a QR code to log in from another device"},
	{prog: "cursor-agent", row: "Authentication required. Please run 'cursor-agent login' to authenticate."},
	{prog: "cursor-agent", row: "Error: Authentication required. Please run 'agent login' first, or set CURSOR_API_KEY environment variable."},
	{prog: "cursor-agent", row: "Authentication failed: your Cursor credentials or API key are invalid or expired. If you set CURSOR_API_KEY, check that it is correct, or run `cursor-agent login` to re-authenticate."},
	{prog: "cursor-agent", row: "Your stored authentication is invalid. Please log in again."},
	{prog: "cursor-agent", row: "⚠ Warning: The provided API key is invalid.", refused: true},

	{prog: "pi", row: " Error: No API key found for the selected model."},
	{prog: "pi", row: " Error: No API key found for anthropic."},
	{prog: "pi", row: ` Error: Authentication failed for "anthropic". Credentials may have expired or network is unavailable. Run '/login anthropic' to re-authenticate.`},
	{prog: "pi", row: " Error: Your session has expired. Please reauthenticate."},
}

// loginRowFrame is what sits under a row in these screens: an input box,
// three rows, so a row above it is not among the last few (the CLI's own
// status), and the plain words read there (harnessLoginLines) stay out of it.
const loginRowFrame = "\n────────────────────────\n❯\n────────────────────────"

// loggedOutBy reports whether tail reads, with prog here, as prog unable to
// run for want of a login, or turning away its key.
func loggedOutBy(tail, prog string) bool {
	return harnessProblemIn(tail, prog) || harnessKeyRefusedBy(tail, prog)
}

// screenWith is a screen with rows where the case reads them: above the
// input box, or (bottom) on the last rows.
func (c loginRowCase) screenWith(rows string) string {
	if c.bottom {
		return "● Done with the build.\n" + rows + "\n❯"
	}
	return "● Done with the build.\n" + rows + loginRowFrame
}

// words is the row's text without the marks the CLI draws before it.
func (c loginRowCase) words() string {
	return strings.TrimLeftFunc(c.row, func(r rune) bool { return strings.ContainsRune(" ■●⎿✕✢›⚠│┃", r) })
}

// halves splits the row's words at the space nearest their middle.
func (c loginRowCase) halves() (string, string) {
	w := strings.TrimRight(c.words(), " │")
	mid := len(w) / 2
	at := strings.LastIndex(w[:mid], " ")
	if after := strings.Index(w[mid:], " "); at < 0 || (after >= 0 && after < mid-at) {
		at = mid + after
	}
	return w[:at], w[at+1:]
}

// Each CLI's own row is read as it is drawn, and wrapped at the terminal's
// width; with that CLI here, and with no program known.
func TestEachCLIsLoginRowIsRead(t *testing.T) {
	for _, c := range loginRowCases {
		tail := harnessTail(c.screenWith(c.row), harnessRows)
		if c.refused {
			if !harnessKeyRefusedBy(tail, c.prog) {
				t.Errorf("%s: its refused key was not read: %q", c.prog, c.row)
			}
			continue
		}
		for _, prog := range []string{c.prog, ""} {
			if !harnessProblemIn(tail, prog) {
				t.Errorf("%s (as %q): not read as logged out: %q", c.prog, prog, c.row)
			}
		}
		if c.notice {
			continue
		}
		first, second := c.halves()
		lead := c.row[:len(c.row)-len(c.words())]
		wrapped := harnessTail(c.screenWith(lead+first+"\n  "+second), harnessRows)
		if !harnessProblemIn(wrapped, c.prog) {
			t.Errorf("%s: wrapped onto two rows, not read as logged out:\n%s\n  %s", c.prog, lead+first, second)
		}
	}
}

// The same words in an agent's own text are not the CLI: a sentence that
// starts with them and carries on, a quote of them in an answer or on a row
// of its own, and a wrapped row of prose that starts in the middle of them
// or carries on after them.
func TestLoginWordsInAnAgentsOwnTextAreNotALogin(t *testing.T) {
	for _, c := range loginRowCases {
		w := c.words()
		first, second := c.halves()
		lead := c.row[:len(c.row)-len(w)]
		for _, s := range []struct{ what, rows string }{
			{"carries on", strings.TrimRight(c.row, " │") + " — so I changed the setting and it works now."},
			{"quoted in an answer", "● The CLI printed \"" + strings.TrimRight(w, " │") + "\" before the fix."},
			{"quoted on its own row", "  \"" + strings.TrimRight(w, " │") + "\""},
			{"wrapped, from its middle", "● Earlier the tool printed " + first + "\n  " + second + " and then went on."},
			{"wrapped, carrying on", lead + first + "\n  " + second + " and then went on."},
		} {
			tail := harnessTail(c.screenWith(s.rows), harnessRows)
			for _, prog := range []string{c.prog, ""} {
				if loggedOutBy(tail, prog) {
					t.Errorf("%s (as %q), %s: read as logged out:\n%s", c.prog, prog, s.what, s.rows)
				}
			}
		}
	}
	// What the wording a CLI uses elsewhere reads like (Codex's own
	// messages that start with its menu's words, and go on).
	for _, s := range []struct{ prog, row string }{
		{"codex", "■ Sign in with ChatGPT auth; API key auth cannot load remote plugin catalogs"},
		{"codex", "■ Couldn't load local sign-in. Sign in with ChatGPT and retry."},
		{"codex", "  Sign in with ChatGPT"},
		{"pi", " No API key found for the deploy step, so I set it in .env."},
	} {
		if loggedOutBy(harnessTail("● Done.\n"+s.row+loginRowFrame, harnessRows), s.prog) {
			t.Errorf("%s: read as logged out: %q", s.prog, s.row)
		}
	}
}

// A row read only on the CLI's own status rows is not one higher up, in the
// conversation.
func TestABottomRowHigherUpIsNotALogin(t *testing.T) {
	for _, c := range loginRowCases {
		if !c.bottom {
			continue
		}
		if loggedOutBy(harnessTail("● Done.\n"+c.row+loginRowFrame, harnessRows), c.prog) {
			t.Errorf("%s: above its input box, read as logged out: %q", c.prog, c.row)
		}
	}
}
