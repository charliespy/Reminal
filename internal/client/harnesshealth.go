// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"regexp"
	"strings"
	"time"
	"unicode"

	"reminal/internal/session"
)

// Whether the agent in this session can work at all. A harness whose login
// has expired still draws its prompt, reads the line typed into it, and ends
// the turn at once with "Login expired · Please run /login": to everything
// that watches turns, the message was delivered and handled. It was not —
// and every later one would go the same way, until a person logs it in.
//
// So the bottom of the screen, where harnesses put their status line and
// such errors, is read on every attention tick, and while it says the agent
// is logged out the session reports that instead of an attention state: a
// session that looks idle is one you leave alone, and this one needs you.

// harnessLoggedOut is the state a session reports while its agent cannot
// work for want of a login. It is not one of the attention states — those say
// what the agent is doing, and this says it can do nothing.
const harnessLoggedOut = "logged-out"

// What agent CLIs print when they cannot run for want of a login. Matched
// with all whitespace removed: Claude Code draws the spaces between words as
// cursor movements, so its screen reads "●Loginexpired·Pleaserun/login" and
// "Notloggedin·Run/login" — and must match whether or not spaces render.
//
// harnessLoginNotice is Claude Code's shape: what went wrong, then the
// command to run. The command in the same breath is what tells it from an
// agent's own prose about logins ("…was not logged in, so the redirect to
// /login looped" does not match).
var harnessLoginNotice = regexp.MustCompile(`(loginexpired|notloggedin|invalidapikey|oauthtoken(has)?expired)[·:.,\-]*(please)?run/login`)

// harnessLoginScreens are the sign-in screens other CLIs start on when they
// have no login — a menu of ways to sign in, not an error. A message typed
// into one picks a menu item or answers its question: Amp's "Would you
// like to log in? [(y)es, (n)o]" took the first letter of a welcome as a
// no, and the login was gone. Each is the screen's own heading, and counts
// only when its own CLI is the program here: a Claude Code session whose tool
// printed Codex's sign-in menu (a check of Codex's login, 2026-10-14) is not
// logged out, and was read as one for as long as that output was on screen.
var harnessLoginScreens = map[string]*regexp.Regexp{
	"agy": regexp.MustCompile(`youarecurrentlynotsignedin`),
	// Gemini CLI: no way to sign in chosen yet; its Google sign-in waiting
	// for the code; its API key dialog; and a key it cannot use (the
	// service's own words, in the error it prints for the turn — wrapped at
	// the terminal's width, so by their start).
	"gemini": regexp.MustCompile(`howwouldyouliketoauthenticate|noauthenticationmethodselected|^[^a-z0-9]*(pleasevisitthefollowingurltoauthorizetheapplication|entertheauthorizationcode|entergeminiapikey)[^a-z0-9]*$|apikeynotvalid\.pleasepass`),
	"amp":    regexp.MustCompile(`wouldyouliketologintoamp|noapikeyfound\.startingloginflow`),
	// Codex: its sign-in menu, signing in through the browser, and a key it
	// cannot use.
	"codex": regexp.MustCompile(`signinwithchatgpt|provideyourownapikey|incorrectapikeyprovided|^[^a-z0-9]*finishsigninginviayourbrowser[^a-z0-9]*$`),
	// Qwen Code: its provider menu (0.24: "Connect a Provider", a dialog
	// taller than the rows read, so by its last choice), and the steps of
	// setting one up.
	"qwen": regexp.MustCompile(`alibabamodelstudio.{0,3}accessmethod|finishprovidersetup|connectwithanexistingmodelstudioapikey|^[^a-z0-9]*(connectaprovider|manuallyconnectalocalserver,proxy,orunsupportedprovider)[^a-z0-9]*$`),
	// Cursor CLI: its first screen with no login ("Press any key to log
	// in..." — a key typed there starts signing in), and the sign-in itself.
	// A key it was given and cannot use is read apart (harnessKeyRefusals):
	// its words are common enough to come from anything.
	"cursor-agent": regexp.MustCompile(`^[^a-z0-9]*(pressanykeytologin|ifyourbrowserdidn.?topen,clickthislinktologin:?|pressqtoshowaqrcodetologin)`),
	// pi with no model to run on (no provider logged in) says so on its own
	// status line, the last on its screen: its context "0" and its model
	// "unknown" ("0.0%/0 (auto)   unknown"). A message sent it is turned away
	// with "Error: No API key found for the selected model." Its startup
	// warning ("No models available. Use /login …") sits above its editor,
	// out of the rows read, and stays there after a /login, so it is not used.
	"pi": regexp.MustCompile(`^\d+(\.\d+)?%/0(\([a-z]+\))?unknown$|^[^a-z0-9]*error:noapikeyfoundfortheselected`),
	// Claude Code's own login menu (a fresh machine shows it before anything
	// else), and its sign-in step once a way is picked: "Opening browser to
	// sign in…" under a spinner first, then the link. Its own rows, from
	// their start: its conversation can name these words in a sentence. In a
	// narrow terminal the link's heading wraps beside "(c to copy)", so only
	// its start is on its first row ("Browser didn't open? Use the url").
	// Not seen during the spinner, a logged-out Claude Code was read as at
	// work, then done.
	"claude": regexp.MustCompile(`^[^a-z0-9]*(\d\.)?(selectloginmethod:?$|claudeaccountwithsubscription·|anthropicconsoleaccount·apiusagebilling|openingbrowsertosignin|browserdidn.?topen\?usetheurl|pastecodehereifprompted)`),
}

// cursorKeyRefused is Cursor CLI turning away the API key it was given, from
// the start of its own row: "⚠ Warning: The provided API key is invalid.",
// then where the key came from ("The API key was loaded from the
// CURSOR_API_KEY environment variable."), and it exits. A key that was
// passed and refused, not a login missing: the key is what to change.
const cursorKeyRefused = `^[^a-z0-9]*(warning:)?theprovidedapikeyisinvalid`

// harnessKeyRefusals are, by CLI, the rows that say it refused the key it was
// given (not that it has none). Unlike harnessLoginScreens they are taken as
// that CLI's only while it is in front or was just before (noteHarnessHealth):
// "The provided API key is invalid" is also what other tools print, and at a
// plain shell's prompt it says nothing about an agent's login.
var harnessKeyRefusals = map[string]*regexp.Regexp{
	"cursor-agent": regexp.MustCompile(cursorKeyRefused),
}

// harnessKeyRefusedIn reports whether the bottom of a screen has a CLI
// saying it refused its key, and which CLI ("" if none): read with the same
// rows as harnessProblemIn. After the CLI exits on it, its words stay above
// the shell's prompt, the shell in front — so the CLI is the one whose
// words they are, not the program here.
func harnessKeyRefusedIn(tail string) string {
	for _, row := range strings.Split(tail, "\n") {
		flat := strings.ToLower(strings.Join(strings.Fields(row), ""))
		if flat == "" {
			continue
		}
		for prog, re := range harnessKeyRefusals {
			if re.MatchString(flat) {
				return prog
			}
		}
	}
	return ""
}

// harnessLoginScreenIn reports whether a row is the sign-in screen of prog,
// the program here; of any of them when it is not known.
func harnessLoginScreenIn(flat, prog string) bool {
	if prog == "" {
		for _, re := range harnessLoginScreens {
			if re.MatchString(flat) {
				return true
			}
		}
		return false
	}
	re := harnessLoginScreens[prog]
	return re != nil && re.MatchString(flat)
}

// harnessLoginLines are other CLIs' notices: they count only on a short row
// that begins with them — a status line, not a sentence in an answer.
var harnessLoginLines = []string{
	"authenticationrequired", "pleaselogin", "pleasesignin", "loginrequired",
	"waitingforauth", "pleasesetanauthmethod", "loginexpired", "notloggedin",
}

// harnessLineRows is where those notices count: the last few rows with
// anything on them — a CLI's own status line and what sits right above its
// prompt. Higher up is the conversation, where an agent's tools print what
// other programs said: a `netlify status` answering "Not logged in. Please
// log in to see site status." is the site's login, not the agent's, and it
// once held the agent's mail and told its supervisor it could not work. The
// sign-in screens and Claude Code's own notice are specific enough to be read
// anywhere in the tail.
const harnessLineRows = 3

// harnessShortRow is how long (whitespace removed) such a row may be.
const harnessShortRow = 48

// harnessRows is how much of the bottom of the screen is read: the status
// line and the notice above the prompt, not the conversation. Counted in
// rows that have something on them — a sign-in dialog is mostly its own
// blank rows, and counting those left its heading unread.
const harnessRows = 12

// harnessRowsFor is how many rows are read for prog: Gemini CLI's input
// area, its status rows and the banners it keeps above them are taller
// than that, and its error for a turn sits above them all.
func harnessRowsFor(prog string) int {
	if prog == "gemini" {
		return 20
	}
	return harnessRows
}

const (
	harnessDownAfter = 2  // reads the notice must be seen in to count
	harnessUpAfter   = 10 // reads it must be gone from to clear…
	// …or this long, whichever comes first: the screen is read when it may
	// have changed, so an agent that comes up from its sign-in and then sits
	// still at its prompt gives a few reads, not ten — and was left "logged
	// out", its mail held, for good.
	harnessUpFor = 3 * time.Second
)

// harnessNow is the clock the health state is kept by (tests set it).
var harnessNow = time.Now

// harnessHealth is the debounced state; guarded by Agent.metaMu.
type harnessHealth struct {
	down       bool
	seen, gone int
	since      time.Time
	// When the notice was first seen in this run of reads, and when it was
	// first gone: what the next read is timed by (harnessReadAt).
	seenAt, goneAt time.Time
	// refused is the CLI whose words, while down, say it refused the key it
	// was given (harnessKeyRefusedIn), "" when down for want of a login.
	refused string
}

// harnessTail is the last n rows of a screen that have anything on them.
func harnessTail(render string, n int) string {
	var rows []string
	for _, row := range strings.Split(render, "\n") {
		if strings.TrimSpace(row) != "" {
			rows = append(rows, row)
		}
	}
	if len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return strings.Join(rows, "\n")
}

// harnessProblemIn reports whether the bottom of a screen says the agent is
// logged out. prog is the program running here, as the attention probe found
// it ("" when it is not known): only its own sign-in screen, and Claude
// Code's notice only from Claude Code, are its own.
func harnessProblemIn(tail, prog string) bool {
	if !isAgentProgram(prog) {
		// Not one the probe names as an agent ("node", "python3": an agent
		// run from a path it does not know, or a shell): not known, so every
		// CLI's sign-in screen and notice count, as before.
		prog = ""
	}
	var rows []string
	for _, row := range strings.Split(tail, "\n") {
		if flat := strings.ToLower(strings.Join(strings.Fields(row), "")); flat != "" {
			rows = append(rows, flat)
		}
	}
	for i, flat := range rows {
		bottom := i >= len(rows)-harnessLineRows
		if harnessLoginScreenIn(flat, prog) || ((prog == "" || prog == "claude") && harnessNoticeRow(flat, bottom)) {
			return true
		}
		if !bottom {
			continue // the conversation, not the CLI's own status
		}
		lead := strings.TrimLeftFunc(flat, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if len(lead) <= harnessShortRow {
			for _, p := range harnessLoginLines {
				if strings.HasPrefix(lead, p) {
					return true
				}
			}
		}
	}
	return false
}

// harnessNoticeRow reports whether a row is Claude Code saying it is logged
// out. It says so in two places: on a line of its own in the conversation
// ("● Login expired · Please run /login"), and at the right-hand end of its
// status line ("⏵⏵ … Not logged in · Run /login"). An agent quoting the notice
// — a lead telling its owner what it saw on a report's screen — puts it in the
// middle of a sentence, and that is not this agent being logged out: reading
// it as one once marked a working lead logged out and held its mail.
func harnessNoticeRow(flat string, bottom bool) bool {
	core := strings.TrimFunc(flat, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	loc := harnessLoginNotice.FindStringIndex(core)
	if loc == nil {
		return false
	}
	if loc[0] == 0 && loc[1] == len(core) {
		return true // a line of its own
	}
	return bottom && loc[1] == len(core) // the end of the status line
}

// harnessAtWork reports whether the harness's own hook shows a turn under way
// for longer than a logged-out one lasts: one turned away ends at once ("Login
// expired" and done), so a turn still going after harnessTurnProof, with its
// screen still moving (a spinner, output), is an agent that can work —
// whatever the screen's text says of logins. What that could hide: a CLI that
// keeps its hook "working" and its screen moving after its login failed —
// for at most session.HookStateTTL (15 min), past which the hook is not
// believed. Claude Code ends a turn it turned away at once.
func harnessAtWork(hs *session.HookState, now time.Time, settledMs int64) bool {
	if hs == nil || hs.State != "working" {
		return false
	}
	age := now.Sub(hs.TS)
	return age >= harnessTurnProof && age < session.HookStateTTL && settledMs < attnSettleMs
}

// harnessTurnProof is how long a turn must have been going to show a login.
const harnessTurnProof = 10 * time.Second

// noteHarnessHealth is called with each attention tick's screen, the program
// running here ("" when not known), and whether its hook shows it at work.
func (a *Agent) noteHarnessHealth(render, prog string, atWork bool) {
	// The render carries its styling — "\x1b[1mLogin\x1b[m \x1b[1mexpired" —
	// which would sit between the words; the text is what is read.
	tail := harnessTail(stripANSI(render), harnessRowsFor(prog))
	a.metaMu.Lock()
	prev := a.attnPrevFG
	a.metaMu.Unlock()
	refused := ""
	if !atWork {
		// A refused key is the CLI's own words only when that CLI is here,
		// or exited just now and left them above the shell's prompt.
		if r := harnessKeyRefusedIn(tail); r != "" && (r == prog || r == prev) {
			refused = r
		}
	}
	bad := !atWork && (refused != "" || harnessProblemIn(tail, prog))
	now := harnessNow()
	a.metaMu.Lock()
	h := &a.harness
	changed := false
	if atWork && h.down {
		// Seen at work: logged in, now.
		h.down, h.seen, h.gone, changed = false, 0, 0, true
	} else if bad {
		if h.seen == 0 {
			h.seenAt = now
		}
		h.gone = 0
		// A CLI that refused its key and exited (no agent in front now):
		// those are its last words, not a glimpse, and count at once — a
		// read later, the seat was said to have "ended" in between.
		exited := refused != "" && !isAgentProgram(prog)
		if h.seen++; !h.down && (h.seen >= harnessDownAfter || exited) {
			h.down, h.since, changed = true, now, true
		}
	} else {
		if h.gone == 0 {
			h.goneAt = now
		}
		h.seen = 0
		if h.gone++; h.down && (h.gone >= harnessUpAfter || now.Sub(h.goneAt) >= harnessUpFor) {
			h.down, changed = false, true
		}
	}
	if !h.down {
		h.refused = ""
	} else if bad {
		h.refused = refused
	}
	down := h.down
	a.metaMu.Unlock()
	if changed && !down {
		// Logged in again: the next tick's screen reading decides the state.
		a.setAttnState("")
	}
}

// harnessReadAt is when the screen should be read again for the health
// state to settle, whatever else the screen does: a notice seen once is
// confirmed a read later, and one gone from a logged-out screen clears at
// harnessUpFor. Zero when nothing waits on a read.
func (a *Agent) harnessReadAt() time.Time {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	h := &a.harness
	switch {
	case !h.down && h.seen > 0:
		return h.seenAt.Add(attnInterval)
	case h.down && h.gone > 0:
		return h.goneAt.Add(harnessUpFor)
	}
	return time.Time{}
}

// harnessSlowToTurnAway are the agents a turn of which runs for a while
// before it says it cannot run: pi with no model to run on works for over
// 2 s before "Error: No API key found for the selected model." Two seconds of
// work is no proof it took the message: the whole window is waited out.
var harnessSlowToTurnAway = map[string]bool{"pi": true}

// harnessKeyRefused is the CLI that, as the agent here is logged out,
// refused the key it was given ("" when it has no login at all).
func (a *Agent) harnessKeyRefused() string {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	return a.harness.refused
}

// harnessDown reports whether the agent here is logged out.
func (a *Agent) harnessDown() bool {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	return a.harness.down
}

// harnessTurnedAway watches, just after a message was typed in, whether the
// agent turned it away for want of a login — true as soon as it says so;
// false once it is plainly working on it, or after a while.
func (a *Agent) harnessTurnedAway() bool {
	start := time.Now()
	for time.Since(start) < 6*time.Second {
		if a.harnessDown() {
			return true
		}
		a.metaMu.Lock()
		working, prog := a.attnState == "working", strings.TrimPrefix(a.attnFG, "-")
		a.metaMu.Unlock()
		if working && time.Since(start) > 2*time.Second && !harnessSlowToTurnAway[prog] {
			return false
		}
		time.Sleep(attnInterval)
	}
	return a.harnessDown()
}
