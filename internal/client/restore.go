// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"reminal/internal/atrest"
	"reminal/internal/config"
	"reminal/internal/session"
)

// Logical restore: a session that was running when its machine went down
// comes back as itself — the same id and PIN, so every link and every
// viewer's recents still work — with its scrollback, a new shell where the
// old one was, and the coding agent it was running started again on the
// same conversation. The processes are new; what a person was doing is not
// lost. Works on every OS: it needs nothing from the kernel.
//
// While a session runs, its agent keeps a session.Restore record and a copy
// of its scrollback up to date (restoreSaveEvery). The record survives the
// process — a shutdown, a crash, a reboot — and goes only when the session
// is ended on purpose. `reminal restore` (or the daemon, at login) starts a
// headless agent with REMINAL_RESTORE=<id>, which takes the record over.

const (
	restoreSaveEvery = 15 * time.Second
	// restoreSettle is how long a restored session's record keeps saying
	// which agent it had, while the sessions around it are restored.
	restoreSettle = 2 * time.Minute
	envRestore    = "REMINAL_RESTORE"
)

// processCwd is one process's own working directory.
func processCwd(pid int) string {
	if pid <= 0 {
		return ""
	}
	if runtime.GOOS == "windows" {
		return processCwdWindows(pid)
	}
	return shellCwd(pid)
}

// restoreBanner marks, in the scrollback, where the old session ends and
// the restored one begins.
const restoreBanner = "\r\n\x1b[2m── reminal restored this session after its machine restarted ──\x1b[0m\r\n"

// saveRestore writes this session's restore record, and its scrollback when
// there is new output. Called on a timer; a failure costs only freshness.
func (a *Agent) saveRestore() {
	if a == nil || a.term == nil || a.paused.Load() || a.sessionID == "" {
		return
	}
	a.restoreMu.Lock()
	defer a.restoreMu.Unlock()
	a.metaMu.Lock()
	name, cwd := a.name, a.cwd
	a.metaMu.Unlock()
	r := session.Restore{
		ID: a.sessionID, PIN: a.pin, PinHash: a.pinHash, Token: a.token,
		Name: name, Cwd: cwd, Headless: a.headless, SavedAt: time.Now(),
	}
	prog, args, pid, atPrompt := restoreForeground(a.term)
	if _, ok := resumers[prog]; ok {
		r.Fg, r.FgArgs = prog, args
		r.Conv = agentConv(prog, pid, a.sessionID)
		// Resumed where the agent itself runs: an agent keys its
		// conversations by folder, and the session's own cwd is a guess —
		// on Windows, from its youngest helper process (cursor-agent's
		// worker sits in the home folder).
		if c := processCwd(pid); c != "" {
			r.Cwd = c
		}
	}
	now := time.Now()
	if r.Fg != "" {
		a.restoreAgentSeen = true // it is running again; the restore is over
		a.agentLastSeen = now
	}
	sinceAgent := time.Duration(1<<63 - 1)
	if !a.agentLastSeen.IsZero() {
		sinceAgent = now.Sub(a.agentLastSeen)
	}
	// A person typing the agent's quit: what says it was quit, and not
	// stopped by something else.
	a.metaMu.Lock()
	typed := typedToQuit(a.lastInput, a.agentLastSeen, a.agentGoneAt, a.startedAt)
	a.metaMu.Unlock()
	if r.Fg == "" && holdPreviousAgent(a.restoring, a.restoreAgentSeen, atPrompt, typed, time.Since(a.startedAt), sinceAgent) {
		if prev, err := session.ReadRestore(a.sessionID); err == nil {
			r.Fg, r.FgArgs, r.Conv = prev.Fg, prev.FgArgs, prev.Conv
		}
	}
	r.ConvSince = a.convSince(r.Conv, now)
	if err := session.WriteRestore(r); err != nil {
		if errors.Is(err, atrest.ErrCurrentKeyMissing) && !a.saveStalled {
			a.saveStalled = true
			agentNotify("  reminal: this session is not being saved — the key its saved details are protected with is missing; run `reminal doctor --repair-key`\n")
		}
		return // the keystore will not give the key up yet: try next tick
	}
	a.saveStalled = false
	if seq := a.buf.LatestSeq(); seq != a.restoreSeq {
		if p, err := session.RestoreScrollbackPath(a.sessionID); err == nil {
			id := a.sessionID
			if err := a.writeScrollbackDumpTo(p, func(b []byte) ([]byte, error) {
				return session.SealScrollback(id, b)
			}); err == nil {
				a.restoreSeq = seq
			}
		}
	}
}

// holdPreviousAgent reports whether a record with nothing recognised in the
// foreground should go on naming the agent it had.
//
// Something else in front for a moment — a pager the agent opened — is not the
// agent ending, so the record holds. The shell's own prompt IS the agent
// ending, with one exception: just after a restore the prompt is there only
// because the agent has not been started again yet, and sessions restored
// after this one read this record to decide whether they shared its folder
// (see resumePlan).
//
// That exception used to last the whole settle window regardless, so quitting
// the agent within two minutes of a restore left the record still naming it
// and the next restart brought it back from the dead. Once the agent has
// actually been seen running, the exception has served its purpose: a prompt
// after that is a person quitting, and must be recorded as one.
//
// A prompt within agentGoneGrace of the agent last being seen is held too.
// As a machine shuts down, the agent often quits a moment before reminal
// does; a save in that moment saw only the prompt, and the record forgot the
// agent the restart was meant to bring back.
//
// And past that, a prompt with nobody having typed since the agent was last
// seen (typed false) is held for as long as that lasts: an agent is quit by a
// person — /exit, ^C, ^D, then the shell — and one that left with nobody
// typing was stopped by something else, most often a machine going down,
// which may take minutes to reach reminal after it reached the agent. Timed
// alone, the grace ran out first: a restart brought 18 sessions back with a
// bare prompt, their records having forgotten claude. A person
// who quits it is recorded as such at the first save after the grace.
func holdPreviousAgent(restoring, agentSeen, atPrompt, typed bool, since, sinceAgent time.Duration) bool {
	if !atPrompt || sinceAgent < agentGoneGrace || !typed {
		return true
	}
	return restoring && !agentSeen && since < restoreSettle
}

// typedToQuit says a person typed the agent's quit: keys from just before it
// left (quitKeysBefore) or anything after. The keys that quit it — /exit and
// Return, a last ^C or ^D — come a moment before it goes, and a save can land
// between them and its exit, still seeing it running: judged against that
// sighting alone, the quit read as nobody typing and the record kept the
// agent for good. gone is when the attention probe saw it leave (to within
// a fraction of a second); without that, its last sighting stands in. Never
// seen in this run: anything typed since the session started.
func typedToQuit(input, seen, gone, started time.Time) bool {
	if input.IsZero() {
		return false
	}
	ref := seen
	if gone.After(seen) {
		ref = gone
	}
	if ref.IsZero() {
		return input.After(started)
	}
	return input.After(ref.Add(-quitKeysBefore))
}

// quitKeysBefore is how long before an agent leaves the keys that quit it
// may come.
const quitKeysBefore = 3 * time.Second

// agentGoneGrace is how long a prompt goes on naming the agent that just
// left it (see holdPreviousAgent): two saves.
const agentGoneGrace = 2 * restoreSaveEvery

// convSince is since when this session has had conv: kept while it stays
// the same, carried over from the record a restore started from, reset when
// it changes.
func (a *Agent) convSince(conv string, now time.Time) time.Time {
	if conv == "" {
		a.restoreConv, a.restoreConvSince = "", time.Time{}
		return time.Time{}
	}
	if conv != a.restoreConv {
		since := now
		if prev, err := session.ReadRestore(a.sessionID); err == nil && prev.Conv == conv && !prev.ConvSince.IsZero() {
			since = prev.ConvSince
		}
		a.restoreConv, a.restoreConvSince = conv, since
	}
	return a.restoreConvSince
}

func (a *Agent) restoreLoop(stop <-chan struct{}) {
	t := time.NewTicker(restoreSaveEvery)
	defer t.Stop()
	a.saveRestore()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			a.saveRestore()
		}
	}
}

// settleRestore decides, as the session ends, whether it may come back. A
// session ended on purpose — its shell exited by itself — is forgotten; one
// ended by a signal (the machine shutting down, a crash) is kept. `reminal
// kill` and `reminal stop` forget it themselves.
func (a *Agent) settleRestore() {
	if a.stopSignal.Load() {
		return
	}
	if a.term != nil && a.term.EndedBySignal() {
		return
	}
	// Another agent serving this id (its record still there after this one
	// cleared only its own) keeps the session coming back.
	if session.ActiveState(a.sessionID) != session.Gone {
		return
	}
	_ = session.ClearRestore(a.sessionID)
}

// ---- the command that picks the agent up again ------------------------------

type resumer struct {
	byID   func(bin string, flags []string, conv string) []string
	latest func(bin string, flags []string) []string
	// pick opens the agent's own list of conversations to choose from —
	// what is typed when "latest" could be another session's.
	pick func(bin string, flags []string) []string
	// how says, to a person, how to find a conversation again by hand, for
	// an agent with no list to open.
	how string
}

func plainFlags(bin string, flags []string, tail ...string) []string {
	return append(append([]string{bin}, flags...), tail...)
}

// resumers: how each coding agent is told to pick a conversation up again —
// by the id its hook reported when there is one, else its latest in this
// directory. The flags it was started with (a model, a permission mode)
// are kept; a prompt given on the command line is not, or it would be sent
// again.
var resumers = map[string]resumer{
	"claude": {
		byID:   func(b string, f []string, c string) []string { return plainFlags(b, f, "--resume", c) },
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--continue") },
		pick:   func(b string, f []string) []string { return plainFlags(b, f, "--resume") },
	},
	"codex": {
		byID:   func(b string, f []string, c string) []string { return append(append([]string{b, "resume"}, f...), c) },
		latest: func(b string, f []string) []string { return append(append([]string{b, "resume"}, f...), "--last") },
		pick:   func(b string, f []string) []string { return append([]string{b, "resume"}, f...) },
	},
	"cursor-agent": {
		byID:   func(b string, f []string, c string) []string { return plainFlags(b, f, "--resume", c) },
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--continue") },
		pick:   func(b string, f []string) []string { return plainFlags(b, f, "--resume") },
	},
	"gemini": {
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--resume", "latest") },
		how:    "gemini --list-sessions, then gemini --resume <number>",
	},
	"qwen": {
		byID:   func(b string, f []string, c string) []string { return plainFlags(b, f, "--resume", c) },
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--continue") },
		pick:   func(b string, f []string) []string { return plainFlags(b, f, "--resume") },
	},
	"opencode": {
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--continue") },
		how:    "opencode session list, then opencode --session <id>",
	},
	"pi": {
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--continue") },
		how:    "start pi and pick the conversation from its session list",
	},
	"agy": {
		latest: func(b string, f []string) []string { return plainFlags(b, f, "--continue") },
		how:    "start agy and pick the conversation from its session list",
	},
	"amp": {
		latest: func(b string, _ []string) []string { return []string{b, "threads", "continue"} },
		how:    "amp threads list, then amp threads continue <id>",
	},
}

// valueFlags take the next argument as their value — used only when an
// agent's --help could not be read (see restoreflags.go).
var valueFlags = map[string]bool{
	"--model": true, "-m": true, "--permission-mode": true, "--add-dir": true, "--agent": true,
	"--settings": true, "--mcp-config": true, "--allowedTools": true, "--allowed-tools": true,
	"--disallowedTools": true, "--disallowed-tools": true, "--append-system-prompt": true,
	"--fallback-model": true, "--sandbox": true, "--profile": true,
	"--provider": true, "-e": true, "--extension": true, "--thinking": true, "--approval-mode": true,
}

// argRules are what each agent's arguments mean that its --help cannot say:
// which make a run one-shot (nothing to resume), which pick or start a
// conversation (replaced by the resume), and which carry a prompt (never
// sent twice). They differ by agent — -p is claude's print mode and codex's
// profile; -c is claude's continue and codex's config.
type argRules struct {
	oneShot    []string // flags that make a run non-interactive
	oneShotSub []string // subcommands that are not a conversation
	drop       []string // flags a resume replaces, or a prompt rides on
}

var agentRules = map[string]argRules{
	"claude": {oneShot: []string{"-p", "--print"},
		oneShotSub: []string{"mcp", "config", "update", "doctor", "install", "setup-token", "plugin", "migrate-installer"},
		drop:       []string{"--resume", "-r", "--continue", "-c", "--session-id", "--fork-session", "--from-pr"}},
	"codex": {oneShotSub: []string{"exec", "e", "review", "login", "logout", "mcp", "mcp-server", "app-server", "completion", "sandbox", "debug", "apply", "a", "cloud", "features"},
		drop: []string{"--last", "--all"}},
	"cursor-agent": {oneShot: []string{"-p", "--print"},
		oneShotSub: []string{"login", "logout", "status", "whoami", "mcp", "update", "upgrade", "ls", "create-chat", "install-shell-integration", "uninstall-shell-integration"},
		drop:       []string{"--resume", "--continue"}},
	"gemini": {oneShot: []string{"-p", "--prompt", "--list-sessions", "--delete-session", "--list-extensions"},
		oneShotSub: []string{"mcp", "extensions"},
		drop:       []string{"--resume", "-r", "--session-id", "--session-file", "-i", "--prompt-interactive"}},
	"qwen": {oneShot: []string{"-p", "--prompt"},
		oneShotSub: []string{"mcp", "extensions"},
		drop:       []string{"--resume", "-r", "--continue", "-c", "--session-id", "--fork-session", "-i", "--prompt-interactive"}},
	"opencode": {oneShotSub: []string{"run", "serve", "web", "acp", "mcp", "models", "session", "export", "import", "github", "stats", "auth", "upgrade", "agent", "plugin", "uninstall", "debug"},
		drop: []string{"--continue", "-c", "--session", "-s", "--fork", "--prompt"}},
	"pi": {oneShot: []string{"-p", "--print"},
		drop: []string{"--continue", "-c", "--resume", "-r", "--session"}},
}

func inList(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// flagSpecFor is the flag spec an agent's --help gives, asked once per agent
// in a process. Replaced in tests.
var flagSpecFor = func() func(string) flagSpec {
	cache := map[string]flagSpec{}
	return func(bin string) flagSpec {
		if s, ok := cache[bin]; ok {
			return s
		}
		s := helpSpec(bin)
		cache[bin] = s
		return s
	}
}()

// resumeArgv is the command that resumes r's agent, or nil — assuming it is
// the only one of its kind in its folder (see resumePlan).
func resumeArgv(r session.Restore) []string { return resumeArgvFor(r, "") }

// resumeArgvFor is resumeArgv for the shell it will be typed into.
func resumeArgvFor(r session.Restore, shell string) []string {
	rs, ok := resumers[r.Fg]
	if !ok {
		return nil
	}
	flags, _, ok := resumeFlagsFor(r, shellUnsafe(shell))
	if !ok {
		return nil
	}
	if !session.ValidConv(r.Conv) {
		r.Conv = "" // only an id is typed; anything else resumes "the latest"
	}
	if r.Conv != "" && rs.byID != nil {
		return rs.byID(r.Fg, flags, r.Conv)
	}
	if rs.latest != nil {
		return rs.latest(r.Fg, flags)
	}
	return nil
}

// resumePlan is what to type into r's restored shell, and a line to show
// above it, given every other session that is being (or was) restored.
//
// Resuming by id is exact. "Latest" is exact only when r was the one
// session running its agent in its folder: when two claudes shared a
// folder and a reboot ended both, "the latest conversation here" is one
// of theirs for both of them. Then the agent's own list is opened so a
// person picks — or, for an agent with none, it is not started and the
// line says how to find the conversation.
func resumePlan(r session.Restore, peers []session.Restore) (argv []string, note string) {
	return resumePlanFor(r, peers, "")
}

// resumePlanFor is resumePlan for the shell the command is typed into: a flag
// that shell would act on is left out, and the note says which.
func resumePlanFor(r session.Restore, peers []session.Restore, shell string) (argv []string, note string) {
	argv, note = resumePlanBody(r, peers, shell)
	if argv != nil {
		if _, dropped, _ := resumeFlagsFor(r, shellUnsafe(shell)); len(dropped) > 0 {
			left := "left out " + strings.Join(dropped, ", ")
			if note != "" {
				note += "; " + left
			} else {
				note = left
			}
		}
	}
	return argv, note
}

func resumePlanBody(r session.Restore, peers []session.Restore, shell string) (argv []string, note string) {
	if r.Fg == "" {
		return nil, ""
	}
	rs, known := resumers[r.Fg]
	if !known {
		return nil, r.Fg + " was running here; reminal does not know how to resume it — start it again by hand"
	}
	if !session.ValidConv(r.Conv) {
		r.Conv = ""
	}
	if _, ok := resumeFlags(r); !ok {
		return nil, r.Fg + " was running a one-off command here, not a conversation — nothing to resume"
	}
	taken := r.Conv != "" && !ownsConv(r, peers)
	if taken {
		// Another session has this conversation, and had it first: resuming
		// it here too would open one conversation in two sessions. This
		// session's own is not known, so it is picked from the list.
		r.Conv = ""
	}
	argv = resumeArgvFor(r, shell)
	if argv == nil {
		return nil, r.Fg + " was running here; reminal cannot resume it — start it again by hand"
	}
	if (r.Conv != "" && rs.byID != nil) || (!taken && !sharesFolder(r, peers)) {
		return argv, ""
	}
	flags, _, _ := resumeFlagsFor(r, shellUnsafe(shell))
	if rs.pick != nil {
		if taken {
			return rs.pick(r.Fg, flags), "the conversation this session last had is open in another session — pick this session's"
		}
		return rs.pick(r.Fg, flags), "several " + r.Fg + " conversations were running in this folder — pick this session's"
	}
	how := rs.how
	if how == "" {
		how = "start " + r.Fg + " and pick the conversation"
	}
	return nil, r.Fg + " was running here, as it was in another session in this folder; to find this one's conversation: " + how
}

// ownsConv says r is the session to resume r.Conv in: no other record of
// the same agent names it, or r has had it longest (the other picked it up
// later, by continuing "the latest" here). A record that does not say since
// when comes after one that does; between equals, the lower id, so every
// session restored in one pass comes to the same answer.
func ownsConv(r session.Restore, peers []session.Restore) bool {
	for _, p := range peers {
		if p.ID == r.ID || p.Fg != r.Fg || p.Conv != r.Conv {
			continue
		}
		if convBefore(p, r) {
			return false
		}
	}
	return true
}

func convBefore(a, b session.Restore) bool {
	switch {
	case a.ConvSince.IsZero() != b.ConvSince.IsZero():
		return !a.ConvSince.IsZero()
	case !a.ConvSince.Equal(b.ConvSince):
		return a.ConvSince.Before(b.ConvSince)
	}
	return a.ID < b.ID
}

// sharesFolder says another session ran the same agent in r's folder.
func sharesFolder(r session.Restore, peers []session.Restore) bool {
	for _, p := range peers {
		if p.ID != r.ID && p.Fg == r.Fg && p.Cwd != "" && filepath.Clean(p.Cwd) == filepath.Clean(r.Cwd) {
			return true
		}
	}
	return false
}

// resumeFlags are the flags r's agent was started with that its resume
// takes, each with its value; false for a run that was never interactive.
//
// With its --help to go by, a flag is carried only if the resume command
// lists it, and a value only where the help says the flag takes one — so
// neither a flag the resume would refuse nor a prompt ever gets through.
// Without it, only the flags known here are.
func resumeFlags(r session.Restore) ([]string, bool) {
	flags, _, ok := resumeFlagsFor(r, hasControl)
	return flags, ok
}

// resumeFlagsFor is resumeFlags leaving out each flag that, or whose value,
// unsafe refuses (shellUnsafe), and saying which it left out.
func resumeFlagsFor(r session.Restore, unsafe func(string) bool) (flags, dropped []string, ok bool) {
	// Where the program's own arguments start: past its interpreter and the
	// interpreter's own options, to the LAST argument naming its file.
	// cursor-agent runs as `cursor-agent --use-system-ca
	// …/cursor-agent/versions/…/index.js <its args>` — the first mention is a
	// launcher, and --use-system-ca is node's, not cursor's to be given.
	args := r.FgArgs
	start := 0
	for i, a := range args {
		if strings.HasPrefix(a, "-") || !strings.Contains(a, r.Fg) {
			continue
		}
		if i == 0 || isRegularFile(a) {
			start = i + 1
		}
	}
	if start == 0 && len(args) > 0 {
		start = 1
	}
	rules := agentRules[r.Fg]
	spec := flagSpecFor(r.Fg)
	known := func(name string) (takes, ok bool) {
		if spec == nil {
			return valueFlags[name], strings.HasPrefix(name, "-")
		}
		if v, ok := spec[name]; ok {
			return v, true
		}
		// yargs takes --no-<flag> for any boolean it lists.
		if strings.HasPrefix(name, "--no-") {
			if v, ok := spec["--"+strings.TrimPrefix(name, "--no-")]; ok && !v {
				return false, true
			}
		}
		return false, false
	}
	sawPositional := false
	for i := start; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break // what follows is a prompt
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			if !sawPositional && inList(rules.oneShotSub, a) {
				return nil, nil, false
			}
			sawPositional = true
			continue // a prompt, or a subcommand: not carried over
		}
		name, _, hasValue := strings.Cut(a, "=")
		if inList(rules.oneShot, name) {
			return nil, nil, false
		}
		takes, ok := known(name)
		if inList(rules.drop, name) {
			// Its value goes with it: taken when the help says so, or — for
			// an optional one ([id]) — whatever does not look like a flag.
			if !hasValue && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && (takes || spec == nil) {
				i++
			}
			continue
		}
		if !ok {
			// Not a flag this resume takes. Its value, if it has one, is
			// not a flag either, and is dropped as a positional would be.
			continue
		}
		kept := []string{a}
		if takes && !hasValue {
			// Its value is the next argument. Without one — it came last, or
			// another flag follows — it is left out: kept bare, it would take
			// what the resume puts after it (--resume and its id) as its value,
			// and the agent would start a new conversation.
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				dropped = append(dropped, noteSafe(name, 40)+" (no value of its own)")
				continue
			}
			kept = append(kept, args[i+1])
			i++
		}
		if unsafe(kept[0]) || (len(kept) == 2 && unsafe(kept[1])) {
			dropped = append(dropped, noteSafe(name, 40)+" (characters a terminal would act on)")
			continue
		}
		flags = append(flags, kept...)
	}
	return flags, dropped, true
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellCommand is argv as a line for the session's shell to run: POSIX
// quoting, or PowerShell's or cmd's on Windows.
func shellCommand(argv []string, shell string) string {
	base := strings.ToLower(shell[strings.LastIndexAny(shell, `/\`)+1:])
	switch {
	case strings.HasPrefix(base, "pwsh"), strings.HasPrefix(base, "powershell"):
		q := make([]string, len(argv))
		for i, a := range argv {
			if shellSafe.MatchString(a) && !strings.ContainsAny(a, "@") {
				q[i] = a
			} else {
				q[i] = "'" + psQuotes.Replace(a) + "'"
			}
		}
		return strings.Join(q, " ")
	case isCmdShell(shell):
		q := make([]string, len(argv))
		for i, a := range argv {
			if shellSafe.MatchString(a) && !strings.ContainsAny(a, "%") {
				q[i] = a
			} else {
				q[i] = `"` + strings.ReplaceAll(a, `"`, `""`) + `"`
			}
		}
		return strings.Join(q, " ")
	}
	return shellJoin(argv)
}

// shellJoin quotes an argv for a POSIX shell.
func shellJoin(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		if shellSafe.MatchString(a) {
			q[i] = a
		} else {
			q[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(q, " ")
}

// ---- coming back --------------------------------------------------------------

// LoadRestoreState turns a restore record into what a headless agent starts
// from: the session's identity and scrollback, no PTY (a new shell is
// started), and how to work out the command that resumes its agent.
//
// That last is a func, run once the session is up (restoreStart), not here:
// it reads the agent's --help, which under the load of a login restoring
// many sessions at once took longer than the daemon waits for a restored
// session to report that it started — and the daemon gave up on it.
func LoadRestoreState(id string) (*ResumeState, func() (run, note string), error) {
	// The id is this process's from here on, or it does not come back.
	if err := claimForRestore(id); err != nil {
		return nil, nil, err
	}
	r, err := session.ReadRestore(id)
	if err != nil {
		return nil, nil, fmt.Errorf("no restore record for %s: %w", id, err)
	}
	if r.PIN == "" {
		return nil, nil, errors.New("restore record has no PIN")
	}
	st := &ResumeState{SessionID: r.ID, PIN: r.PIN, PinHash: r.PinHash, Token: r.Token,
		StartedAt: time.Now(), Name: r.Name, Headless: true}
	if p, err := session.RestoreScrollbackPath(r.ID); err == nil {
		id := r.ID
		st.Dump = readScrollbackDump(p, func(b []byte) ([]byte, error) {
			return session.OpenScrollback(id, b)
		})
	}
	if st.Dump == nil {
		// Not sealed yet: an older version's plain copy, read as it is (it
		// is sealed, and the plain copy removed, at this session's first
		// save).
		if p, err := session.LegacyScrollbackPath(r.ID); err == nil {
			st.Dump = readScrollbackDump(p, func(b []byte) ([]byte, error) { return b, nil })
		}
	}
	plan := func() (string, string) {
		peers, _ := session.ReadRestores()
		sh := config.Shell()
		argv, note := resumePlanFor(*r, peers, sh)
		if argv == nil {
			return "", note
		}
		if why := resumeTooBig(*r); why != "" {
			return "", why
		}
		return shellCommand(lowPriority(argv, sh), sh), note
	}
	return st, plan, nil
}

// restoreStart runs once the new shell is up: the banner, then — when an
// agent was running — the command that resumes it, typed at the prompt.
func (a *Agent) restoreStart() {
	// Not before the session's first try at the relay is over (registered,
	// or the relay unreachable or refusing): resuming a long conversation is
	// seconds of CPU (claude --resume of a 300 MB one is ~20 s on a quarter
	// of a CPU), and started first it starved the session's own
	// registration, which the process that started it waits for, with a
	// deadline (spawnHandshakeTimeout). The session is up at once, its
	// agent resumes behind it.
	<-a.resumeReady()
	if a.restorePlan != nil {
		a.restoreRun, a.restoreNote = a.restorePlan()
	}
	a.record([]byte(restoreBanner))
	if a.restoreNote != "" {
		a.record([]byte("\x1b[2m" + a.restoreNote + "\x1b[0m\r\n"))
	}
	run := a.restoreRun
	if run == "" {
		return
	}
	// The prompt is drawn when the shell has printed and gone quiet.
	start := a.buf.LatestSeq()
	last, quietSince := start, time.Now()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if s := a.buf.LatestSeq(); s != last {
			last, quietSince = s, time.Now()
		}
		if last != start && time.Since(quietSince) > 700*time.Millisecond {
			break
		}
	}
	// Nothing but text is ever typed: the plan leaves out what holds a
	// control character, and this is the last word on it.
	var body, tail []byte
	err := errors.New("its command held characters a terminal would act on")
	if !hasControl(run) {
		if body, tail, err = PrepareInjectKeysSplit(run, true); err == nil {
			err = a.injectKeys(body)
		}
	}
	if err != nil {
		// Never silent: the person is told what would have resumed it — as
		// text, never anything a terminal would act on.
		a.record([]byte("\x1b[2mreminal could not start it again (" + noteSafe(err.Error(), 120) + "); to resume: " + noteSafe(run, 300) + "\x1b[0m\r\n"))
		return
	}
	if tail != nil {
		time.Sleep(EnterSettle)
		_ = a.injectKeys(tail)
	}
}

// Restorable is a session that can be brought back: a record whose session
// is not running — checked one by one (runningElsewhere), so a record that
// cannot be read, or an agent alive under its id, keeps it out. atrest.ErrLocked
// comes back WITH the records it could open when the keystore would not give
// the key up for the rest yet.
func Restorable() ([]session.Restore, error) {
	all, err := session.ReadRestores()
	if err != nil && !errors.Is(err, atrest.ErrLocked) {
		return nil, err
	}
	var out []session.Restore
	for _, r := range all {
		if runningElsewhere(r.ID) == "" {
			out = append(out, r)
		}
	}
	return out, err
}

// RestoreSession starts a headless agent that takes r over. Its shell starts
// where the old one was, or at home when that directory is gone.
func RestoreSession(r session.Restore) (*SpawnedSession, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer devnull.Close()
	cmd := exec.Command(exe, "--headless")
	cmd.Env = spawnEnv(envRestore + "=" + r.ID)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, devnull, devnull
	if dir, err := resolveSpawnDir(r.Cwd); err == nil && dir != "" {
		cmd.Dir = dir
	} else if home, err := os.UserHomeDir(); err == nil {
		cmd.Dir = home
	}
	recv, afterStart, err := prepareHandshake(cmd)
	if err != nil {
		return nil, err
	}
	// On a machine that names a session user, the session runs as it, not
	// as the root reminal runs as there (sessionuser_unix.go).
	if err := asSessionUser(cmd); err != nil {
		afterStart()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		afterStart()
		return nil, err
	}
	afterStart()
	reapDetached(cmd)
	line, err := recv(spawnHandshakeTimeout)
	if err != nil {
		return nil, err
	}
	sp := &SpawnedSession{}
	if err := json.Unmarshal([]byte(line), sp); err != nil {
		return nil, fmt.Errorf("parse handshake: %w", err)
	}
	if len(sp.Phases) > 0 {
		fmt.Printf("reminal: restored %s: load %d ms, agent %d ms, replay %d ms, register %d ms\n",
			sp.ID, sp.Phases["load"], sp.Phases["agent"], sp.Phases["replay"], sp.Phases["register"])
	}
	return sp, nil
}

// RestoreEnvID is the session a headless agent was started to restore, or "".
// It is taken out of the environment as it is read: the session's shell, and
// everything started from it, inherit this process's environment, and a
// `reminal new` run there would otherwise come up as this session again.
func RestoreEnvID() string {
	id := strings.ToUpper(strings.TrimSpace(os.Getenv(envRestore)))
	_ = os.Unsetenv(envRestore)
	return id
}

// restoreAtStart brings back every session a restart ended. REMINAL_NO_RESTORE=1
// turns it off (they can still be restored by hand).
//
// Saved sessions are sealed (see atrest). When the keystore will not give the
// key up yet — a Keychain not unlocked, a desktop keyring locked until the
// person logs in — the records are left as they are and tried again every
// restoreRetryEvery, so the sessions come back once it opens.
func restoreAtStart() {
	sweepHandoffDumps()
	if dir, err := reminalDir(); err == nil {
		atrest.SweepTemps(dir, 10*time.Minute)
	}
	session.QuarantinedRestores() // prune past their keep
	if dir, err := reminalDir(); err == nil {
		atrest.PruneQuarantine(dir) // a set-aside owner key, same keep
	}
	if os.Getenv("REMINAL_NO_RESTORE") == "1" {
		return
	}
	deadline := time.Now().Add(restoreRetryFor)
	tried := map[string]bool{} // a session is tried once; the retries are for locked records
	for {
		gone, err := Restorable()
		for _, r := range gone {
			if tried[r.ID] {
				continue
			}
			tried[r.ID] = true
			if _, err := RestoreSession(r); err != nil {
				// A handshake that timed out may still have brought it back,
				// slowly — or something else did: running is not a failure.
				if runningElsewhere(r.ID) != "" {
					continue
				}
				agentNotify("  reminal: could not restore session %s: %v\n", r.ID, err)
			}
		}
		if !errors.Is(err, atrest.ErrLocked) || time.Now().After(deadline) {
			return
		}
		time.Sleep(restoreRetryEvery)
	}
}

const (
	restoreRetryEvery = 30 * time.Second
	restoreRetryFor   = 24 * time.Hour
)

// sweepHandoffDumps removes hot-restart dumps no successor ever picked up (a
// crash mid-restart). They are single-use; one older than a few minutes will
// never be read. Plain ones from before sealing go the same way.
func sweepHandoffDumps() {
	dir, err := reminalDir()
	if err != nil {
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		n := e.Name()
		if !strings.HasPrefix(n, "scrollback-") {
			continue
		}
		if fi, err := e.Info(); err == nil && time.Since(fi.ModTime()) > 5*time.Minute {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
}

func isRegularFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

func init() {
	session.PINFromSession = func(pid int) string {
		if pid == os.Getpid() {
			return "" // never ask ourselves
		}
		pin, err := sendControlToDeadline(pid, "pin", time.Second)
		if err != nil {
			return ""
		}
		return pin
	}
	// The sealing package reports a fallback taken or a record set aside —
	// on stderr, never stdout, which `reminal mcp` speaks its protocol on.
	atrest.Logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, "  "+format+"\n", args...) }
}
