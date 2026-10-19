// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Whether the agent in this session has run out of what its service lets it
// use: a usage limit, a rate limit, a quota. A harness at its limit takes a
// message, answers it with "Usage limit reached · resets 1:30pm" and sits at
// its prompt: to everything that watches turns it is done, waiting for the
// next message. Every message sent it goes the same way until the limit
// resets, and nothing comes back by itself once it has: someone has to send
// it on.
//
// So, as for a login (harnesshealth.go), the bottom of the screen is read on
// every attention tick, and while it carries the harness's own limit notice
// the session reports harnessLimited, and when the notice says, the time the
// limit resets. Detection only: nothing is typed.

// harnessLimited is the state a session reports while its agent is held back
// by its service's limit. Like harnessLoggedOut it says the agent can do
// nothing now, and outranks what it looked like it was doing.
const harnessLimited = "limited"

// limitNotice is one harness's limit notice: what its row reads as from its
// first letter or digit, lowercase, all whitespace removed (as the login
// notices are read: Claude Code draws its spaces as cursor movements).
type limitNotice struct {
	re *regexp.Regexp
}

func notices(res ...string) []limitNotice {
	out := make([]limitNotice, len(res))
	for i, r := range res {
		out[i] = limitNotice{regexp.MustCompile("^(?:" + r + ")")}
	}
	return out
}

// harnessLimitNotices are, by CLI, the rows that say it is at its limit —
// its own words, matched from the row's start (after the glyphs it draws
// before them: "⎿", "●", "■", "✕"). A sentence that quotes them is not them,
// nor is a row at the input prompt: a person's own text, or the conversation
// repeating what a person typed, starts with the prompt's ">" or "›".
//
// What each entry was checked against — a screen captured from the CLI
// itself, or only its source — is in harnesslimit_test.go.
var harnessLimitNotices = map[string][]limitNotice{
	// Claude Code 2.1.303: "You've hit your session limit · resets 3pm
	// (America/Los_Angeles)" — session, weekly, Opus, Sonnet, usage credit,
	// monthly spend, fast; "· progress saved" after it. Its team's or org's
	// budget, credits run out, and the plain headings.
	"claude": notices(
		`you.?vehityour(session|weekly|opus|sonnet|fable|usagecredit|usage|monthlyspend|fast)?limit`,
		`you.?vehityour(team|org).?s(shared|monthly)`,
		`you.?reoutofusagecredits`,
		`yourorgisoutofusage`,
		`usagelimitreached($|·)`,
		`paused·usagelimitresetsat`,
		`fastlimitreachedandtemporarilydisabled`,
		`goalpaused·usagelimitreached`,
	),
	// Codex 0.163: "You’ve hit your usage limit. … or try again at 3:05 PM."
	// (the time on the next row when it wraps), its workspace's limit and
	// credits, and the service's quota.
	"codex": notices(
		`you.?vehityourusagelimit`,
		`usagelimitreached\.you.?vereachedyourusagelimit`,
		`you.?reoutofcredits\.yourworkspace`,
		`quotaexceeded\.checkyourplanandbilling`,
	),
	// Gemini CLI 0.64: "Usage limit reached for gemini-2.5-pro.", then
	// "Access resets at 3:05 PM PDT." on a row of its own; its daily quota.
	"gemini": notices(
		`usagelimitreachedfor`,
		`youhaveexhaustedyourdailyquota`,
	),
	// Qwen Code 0.25: the service's 429 in its error line, and what it adds.
	"qwen": notices(
		`apierror:.*\(status:429\)`,
		`possiblequotalimitationsinplace`,
		`quotaexhausted:`,
	),
	// OpenCode 1.19: "Usage limit reached. It will reset in 3 hours 20
	// minutes." (a model's name may lead it), its Go plan's, a rate limit.
	"opencode": notices(
		`[a-z0-9.\-]*usagelimitreached\.itwillresetin`,
		`golimitreached`,
		`freeusageexceeded`,
		`ratelimitexceeded,retryin`,
		`quotaexceeded\.checkyourplanandbilling`,
	),
	// goose 1.55: its own words, bare or inside "Ran into this error: …".
	"goose": notices(
		`(ranintothiserror:)?ratelimited—waitamoment`,
		`(ranintothiserror:)?ratelimitexceeded:`,
		`(ranintothiserror:)?creditsexhausted:`,
	),
	// Crush 0.98 has no notice of its own: under "Provider Error" it shows
	// the service's message. OpenAI's and Anthropic's words for a limit.
	"crush": notices(
		`youexceededyourcurrentquota`,
		`ratelimitexceeded`,
		`(error:)?rate_limit_error`,
	),
	// Cursor CLI: the limit's words come from its service; the client's own,
	// for a usage or rate limit, are what to do about it.
	"cursor-agent": notices(
		`upgradeyourplantocontinue`,
	),
}

// limitPromptGlyphs are what an input prompt, or a person's message echoed
// in the conversation, starts with: a row that starts with one is a person's
// text, never the harness's notice.
const limitPromptGlyphs = ">›❯"

// harnessLimitIn reports whether the bottom of a screen has the CLI here
// saying it is at its limit, and when that resets (zero when it does not
// say, or the time cannot be read). prog is the program running here; ""
// when not known, and then every CLI's notices count.
func harnessLimitIn(tail, prog string, now time.Time) (bool, time.Time) {
	n, at := harnessLimitNoticesIn(tail, prog, now)
	return n > 0, at
}

// harnessLimitNoticesIn is harnessLimitIn with how many notices the screen
// shows: a notice from before the agent worked again stays in its
// conversation, and only one more than those is news.
func harnessLimitNoticesIn(tail, prog string, now time.Time) (int, time.Time) {
	if !isAgentProgram(prog) {
		prog = ""
	}
	count, at := 0, time.Time{}
	rows := strings.Split(tail, "\n")
	for i, row := range rows {
		flat := strings.ToLower(strings.Join(strings.Fields(row), ""))
		if flat == "" {
			continue
		}
		lead := strings.IndexFunc(flat, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) })
		if lead < 0 {
			continue
		}
		if strings.ContainsAny(flat[:lead], limitPromptGlyphs) {
			continue // a person's text, at the prompt or echoed
		}
		core := flat[lead:]
		if !limitRowIn(core, prog) {
			continue
		}
		// The lowest notice is the latest: its reset time is the one that
		// stands. A long one wraps, and Gemini CLI says the time on a row
		// of its own: the two rows below it are read with it.
		count++
		said := core
		for _, next := range rows[i+1 : min(i+3, len(rows))] {
			said += strings.ToLower(strings.Join(strings.Fields(next), ""))
		}
		if t := parseLimitReset(said, now); !t.IsZero() {
			at = t
		}
	}
	return count, at
}

// limitRowIn reports whether a row (from its first letter or digit) is a
// limit notice of prog; of any CLI when prog is "".
func limitRowIn(core, prog string) bool {
	match := func(ns []limitNotice) bool {
		for _, n := range ns {
			if n.re.MatchString(core) {
				return true
			}
		}
		return false
	}
	if prog != "" {
		return match(harnessLimitNotices[prog])
	}
	for _, ns := range harnessLimitNotices {
		if match(ns) {
			return true
		}
	}
	return false
}

// ---- when it resets ---------------------------------------------------------

// A notice says when the limit resets in one of three ways, each read from
// the flattened row (no spaces: "resets1:30pm(america/los_angeles)"):
//   - a clock time, perhaps a date and a zone: "resets 1:30pm",
//     "resets at 2pm (Europe/London)", "try again at 3:05 PM",
//     "resets Oct 20, 1pm";
//   - a wait: "try again in 2h 5m", "in 4 days 2 hours 5 minutes";
//   - an instant: "2026-10-20T14:00:00Z", "2026-10-20 14:00 UTC".
var (
	limitISORe   = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})[t ]?(\d{1,2}:\d{2}(?::\d{2})?)(z|utc|[+-]\d{2}:?\d{2})?`)
	limitWaitRe  = regexp.MustCompile(`(?:tryagain|retry|resets?|available)(?:in|after)((?:\d+(?:days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s),?(?:and)?)+)`)
	limitWaitPt  = regexp.MustCompile(`(\d+)(days?|d|hours?|hrs?|h|minutes?|mins?|m|seconds?|secs?|s)`)
	limitClockRe = regexp.MustCompile(`(?:resets?|resetsat|resetat|tryagainat|availableat|at)(?:on)?(?:(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*(\d{1,2})(?:st|nd|rd|th)?,?(\d{4})?(?:at)?)?(\d{1,2})(?::(\d{2}))?(am|pm)?(?:\(([a-z0-9_+\-/]+)\)|(utc|gmt|[pmce][sd]t|bst|cet|cest))?`)
)

// parseLimitReset reads when a limit resets from a flattened notice row; zero
// when it does not say, or says it in a way not read here.
func parseLimitReset(flat string, now time.Time) time.Time {
	if m := limitISORe.FindStringSubmatch(flat); m != nil {
		if t, ok := limitISO(m, now); ok {
			return t
		}
	}
	if m := limitWaitRe.FindStringSubmatch(flat); m != nil {
		var d time.Duration
		for _, p := range limitWaitPt.FindAllStringSubmatch(m[1], -1) {
			n, _ := strconv.Atoi(p[1])
			switch p[2][0] {
			case 'd':
				d += time.Duration(n) * 24 * time.Hour
			case 'h':
				d += time.Duration(n) * time.Hour
			case 'm':
				d += time.Duration(n) * time.Minute
			case 's':
				d += time.Duration(n) * time.Second
			}
		}
		if d > 0 {
			return now.Add(d).Truncate(time.Second)
		}
	}
	for _, m := range limitClockRe.FindAllStringSubmatch(flat, -1) {
		if t, ok := limitClock(m, now); ok {
			return t
		}
	}
	return time.Time{}
}

func limitISO(m []string, now time.Time) (time.Time, bool) {
	clock := m[2]
	if strings.Count(clock, ":") == 1 {
		clock += ":00"
	}
	loc := now.Location()
	switch z := m[3]; {
	case z == "z" || z == "utc":
		loc = time.UTC
	case z != "":
		z = strings.Replace(z, ":", "", 1)
		sign := 1
		if z[0] == '-' {
			sign = -1
		}
		h, _ := strconv.Atoi(z[1:3])
		mm, _ := strconv.Atoi(z[3:5])
		loc = time.FixedZone("", sign*(h*3600+mm*60))
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", m[1]+" "+clock, loc)
	return t, err == nil
}

var limitMonths = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April,
	"may": time.May, "jun": time.June, "jul": time.July, "aug": time.August,
	"sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

// limitClock reads "1:30pm", "13:30", "oct20,1pm(europe/london)", "oct21st,
// 20263:05pm", "3:05pmpdt": a clock time with no date is its next one from
// now, in the zone named, else the machine's own.
//
// m: month, day, year, hour, minute, am/pm, zone (named), zone (short).
func limitClock(m []string, now time.Time) (time.Time, bool) {
	h, err := strconv.Atoi(m[4])
	if err != nil {
		return time.Time{}, false
	}
	mins := 0
	if m[5] != "" {
		mins, _ = strconv.Atoi(m[5])
	}
	switch m[6] {
	case "am":
		if h == 12 {
			h = 0
		}
	case "pm":
		if h != 12 {
			h += 12
		}
	default:
		// No am/pm: only a 24-hour time with minutes ("13:30") says a time;
		// a bare number ("at 2") is not one.
		if m[5] == "" {
			return time.Time{}, false
		}
	}
	if h > 23 || mins > 59 {
		return time.Time{}, false
	}
	loc := now.Location()
	if m[7] != "" {
		if l := limitZone(m[7]); l != nil {
			loc = l
		}
	} else if m[8] != "" {
		loc = limitShortZone(m[8], now)
	}
	local := now.In(loc)
	y, mo, d := local.Date()
	dated := false
	if mon, ok := limitMonths[m[1]]; ok && m[2] != "" {
		day, _ := strconv.Atoi(m[2])
		mo, d, dated = mon, day, true
		if m[3] != "" {
			y, _ = strconv.Atoi(m[3])
		}
	}
	t := time.Date(y, mo, d, h, mins, 0, 0, loc)
	if dated {
		if m[3] == "" && t.Before(local.Add(-24*time.Hour)) {
			t = t.AddDate(1, 0, 0) // "Jan 2" read on Dec 31
		}
		return t, true
	}
	if !t.After(local) {
		t = t.AddDate(0, 0, 1) // already past today: tomorrow's
	}
	return t, true
}

// limitShortZones are the short zone names a notice may give (Gemini CLI's
// "3:05 PM PDT"), as offsets from UTC.
var limitShortZones = map[string]int{
	"utc": 0, "gmt": 0, "bst": 1, "cet": 1, "cest": 2,
	"est": -5, "edt": -4, "cst": -6, "cdt": -5, "mst": -7, "mdt": -6, "pst": -8, "pdt": -7,
}

// limitShortZone is a short zone name as a location: the machine's own when
// its zone goes by that name now (so its rules apply), else the fixed offset.
func limitShortZone(name string, now time.Time) *time.Location {
	if n, _ := now.Zone(); strings.EqualFold(n, name) {
		return now.Location()
	}
	if off, ok := limitShortZones[name]; ok {
		if off == 0 {
			return time.UTC
		}
		return time.FixedZone(strings.ToUpper(name), off*3600)
	}
	return now.Location()
}

// limitZone is the zone a notice names, as the row's lowercase spells it:
// "america/los_angeles" is America/Los_Angeles, "utc" is UTC.
func limitZone(name string) *time.Location {
	if name == "utc" || name == "gmt" || name == "etc/utc" {
		return time.UTC
	}
	parts := strings.Split(name, "/")
	for i, p := range parts {
		words := strings.Split(p, "_")
		for j, w := range words {
			if w != "" {
				words[j] = strings.ToUpper(w[:1]) + w[1:]
			}
		}
		parts[i] = strings.Join(words, "_")
	}
	if l, err := time.LoadLocation(strings.Join(parts, "/")); err == nil {
		return l
	}
	return nil
}

// ---- the state, debounced ---------------------------------------------------

// harnessLimitState is the debounced limit state; guarded by Agent.metaMu.
// Like the login state: seen in harnessDownAfter reads to count, and cleared
// once the agent is seen at work, or the notice has been gone harnessUpAfter
// reads or harnessUpFor.
type harnessLimitState struct {
	limited        bool
	resetsAt       time.Time
	since          time.Time // when it was found limited
	seen, gone     int
	seenAt, goneAt time.Time
	// stale is how many notices on the screen are from before the agent
	// worked again: they stay in its conversation, and are not news.
	stale int
}

// noteHarnessLimit is called with each attention tick's screen tail (the
// rows harnessTail kept), the program here, whether its hook shows it at
// work (harnessAtWork), and when its hook last said a turn began (zero if it
// has not). It reports whether the limit state changed.
//
// The limit is over the moment a turn begins after it was found: the agent
// took a message and is working. If the limit still holds, the harness
// answers that turn with a notice of its own — one more than the screen
// had, which is what counts as the limit again; the old ones stay in the
// conversation above and are not.
func (a *Agent) noteHarnessLimit(tail, prog string, atWork bool, turnAt time.Time) bool {
	now := harnessNow()
	n, at := harnessLimitNoticesIn(tail, prog, now)
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	l := &a.limit
	if n < l.stale {
		l.stale = n // scrolled away
	}
	hit := !atWork && n > l.stale
	changed := false
	switch {
	case l.limited && (atWork || turnAt.After(l.since)):
		// At work again: the limit is over, whatever its notice said.
		*l, changed = harnessLimitState{stale: n}, true
	case hit:
		if l.seen == 0 {
			l.seenAt = now
		}
		l.gone = 0
		if l.seen++; !l.limited && l.seen >= harnessDownAfter {
			l.limited, l.since, changed = true, now, true
		}
		if l.limited && !at.IsZero() && !at.Equal(l.resetsAt) {
			l.resetsAt, changed = at, true
		}
	default:
		if l.gone == 0 {
			l.goneAt = now
		}
		l.seen = 0
		if l.gone++; l.limited && (l.gone >= harnessUpAfter || now.Sub(l.goneAt) >= harnessUpFor) {
			*l, changed = harnessLimitState{stale: n}, true
		}
	}
	return changed
}

// harnessLimitedNow reports whether the agent here is at its limit, and
// when that resets (zero when not said).
func (a *Agent) harnessLimitedNow() (bool, time.Time) {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	return a.limit.limited, a.limit.resetsAt
}

// harnessLimitReadAt is when the screen should be read again for the limit
// state to settle (as harnessReadAt for the login); zero when nothing waits.
func (a *Agent) harnessLimitReadAt() time.Time {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	l := &a.limit
	switch {
	case !l.limited && l.seen > 0:
		return l.seenAt.Add(attnInterval)
	case l.limited && l.gone > 0:
		return l.goneAt.Add(harnessUpFor)
	}
	return time.Time{}
}
