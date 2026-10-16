// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
	"reminal/internal/atrest"
	"reminal/internal/config"
	"reminal/internal/protocol"
	"reminal/internal/session"
	"reminal/internal/updater"
)

// Doctor runs a series of environment checks and prints a color-coded report.
// It's meant for users who want to confirm reminal is set up correctly or
// debug why something isn't working — equivalent in spirit to `brew doctor`
// or `docker info`.
func Doctor(currentVersion string) error {
	fmt.Println()
	fmt.Println("  reminal doctor")
	fmt.Println()

	worst := levelOK
	for _, c := range allChecks(currentVersion) {
		lvl, summary := c.run()
		fmt.Printf("  %s  %-14s %s\n", badge(lvl), c.name, summary)
		if lvl > worst {
			worst = lvl
		}
	}

	fmt.Println()
	switch worst {
	case levelOK:
		fmt.Println("  All good. Run `reminal` to start sharing.")
	case levelWarn:
		fmt.Println("  Mostly good — warnings above are non-blocking but worth a look.")
	case levelFail:
		fmt.Println("  Something needs fixing. Address the FAIL lines above.")
		fmt.Println("  Stuck? Open an issue with this output:")
		fmt.Println("    https://github.com/harshalgajjar/Reminal/issues")
	}
	fmt.Println()
	if worst == levelFail {
		return errors.New("doctor: one or more checks failed")
	}
	return nil
}

type level int

const (
	levelOK level = iota
	levelWarn
	levelFail
)

type check struct {
	name string
	run  func() (level, string)
}

func allChecks(currentVersion string) []check {
	return []check{
		{"Version", func() (level, string) { return checkVersion(currentVersion) }},
		{"Relay", checkRelay},
		{"WebSocket", checkRelayWS},
		{"Terminal", checkTerminal},
		{"Shell", checkShell},
		{"Active session", checkActiveSession},
		{"Config dir", checkConfigDir},
		{"Saved sessions", checkSavedSessions},
		{"At-rest key", checkAtRestKey},
		{"Owner key", checkOwnerKey},
		{"Window notes", checkWindowNotes},
		{"Phone alerts", checkPhoneAlerts},
	}
}

// checkPhoneAlerts says how this machine's phone alerts stand: phones whose
// alerts are not yet bound to it (forwarded until pushUntokenedUntil, then
// not, until each phone renews them — unless the relay is one that does not
// bind alerts at all, an older self-hosted one), and a clock far enough off
// the relay's that it refuses bound alerts. Phones of owners removed from
// this machine are not counted: they get nothing from it anyway.
func checkPhoneAlerts() (level, string) {
	n := pushSubsToRenew()
	if n < 0 {
		return levelOK, "no phones"
	}
	binds, skew := relayAlertsInfo()
	var notes []string
	lvl := levelOK
	if n > 0 && binds {
		phones := fmt.Sprintf("%d phone(s)", n)
		lvl = levelWarn
		if time.Now().Before(pushUntokenedUntil) {
			notes = append(notes, fmt.Sprintf("%s need alerts renewed before %s: open reminal on each and tap Renew alerts on this phone", phones, pushUntokenedUntil.Format("2 Jan 2006")))
		} else {
			notes = append(notes, fmt.Sprintf("%s have not got alerts from this machine since %s: open reminal on each and turn alerts on again", phones, pushUntokenedUntil.Format("2 Jan 2006")))
		}
	}
	if skew > pushClockSlack || skew < -pushClockSlack {
		lvl = levelWarn
		notes = append(notes, fmt.Sprintf("this machine's clock is %s off the relay's, so the relay refuses its bound alerts: set the clock right", skew.Round(time.Second).Abs()))
	}
	if len(notes) == 0 {
		if n > 0 {
			return levelOK, "none to renew (this relay does not bind alerts to machines)"
		}
		return levelOK, "none to renew"
	}
	return lvl, strings.Join(notes, "; ")
}

// pushClockSlack is how far off the relay's clock a bound alert's time may
// be (cloudflare/src/push.ts's SIG_FRESH), less a margin.
const pushClockSlack = 90 * time.Second

// relayAlertsInfo asks the relay whether it binds alerts to machines (it
// answers POST /push/token; an older one does not know the path) and how
// far this machine's clock is from its own (its Date). Unreachable: taken
// as binding, with no skew known.
func relayAlertsInfo() (binds bool, skew time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.WebURL()+"/push/token", nil)
	if err != nil {
		return true, 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return true, 0
	}
	resp.Body.Close()
	if at, err := http.ParseTime(resp.Header.Get("Date")); err == nil {
		skew = time.Since(at)
	}
	// A relay that binds alerts answers a GET there with "POST only".
	return resp.StatusCode == http.StatusMethodNotAllowed, skew
}

// checkOwnerKey reports how this device's owner identity is kept.
func checkOwnerKey() (level, string) {
	switch OwnerKeyState() {
	case "none":
		return levelOK, "none yet (made by `reminal own`)"
	case "sealed":
		return levelOK, "encrypted on disk"
	case "plain":
		return levelWarn, "not yet encrypted; it is encrypted at its next use"
	case "locked":
		// A check probes nothing, so a locked keystore and a removed key look
		// the same here; owner commands themselves tell them apart.
		return levelWarn, "encrypted, but its keystore can't be reached right now or no longer has the key; owner commands say which, and `reminal own reset` is the way out if it never comes back"
	case "conflict":
		return levelFail, "device_ed25519 and device_ed25519.sealed hold different keys; move the one you don't want aside, or `reminal own reset`"
	}
	return levelFail, "can't be opened; owner commands will fail. `reminal own reset` makes a new identity (re-enrol it on each machine)"
}

// checkSavedSessions says where the key sealing saved sessions lives, and
// whether any saved session could not be opened and was set aside.
func checkSavedSessions() (level, string) {
	where := map[string]string{
		"keychain":       "sealed with a key in the login Keychain",
		"dpapi":          "sealed with a key protected by Windows (DPAPI)",
		"secret-service": "sealed with a key in the desktop keyring",
		"file":           "sealed with a key in a file in ~/.reminal (no OS keystore here)",
	}[atrest.Backend()]
	if where == "" {
		where = "none saved yet"
	} else {
		// A check writes nothing, so "locked" and "gone" cannot be told
		// apart on every OS; one sentence covers both.
		switch ks := atrest.KeyState(); {
		case ks.Missing || ks.MaybeMissing:
			where += ", which no longer has the key; saving is paused until it is back (see At-rest key below)"
		default:
			if st := atrest.Status(); st == "locked" || st == "gone" {
				where += ", which can't be reached right now (saving with a key file meanwhile)"
			}
		}
	}
	if n := session.QuarantinedRestores(); n > 0 {
		return levelWarn, fmt.Sprintf("%s; %d could not be opened and are kept in ~/.reminal/restore/quarantine for 7 days", where, n)
	}
	return levelOK, where
}

func badge(l level) string {
	switch l {
	case levelOK:
		return "\x1b[32m[ OK ]\x1b[0m"
	case levelWarn:
		return "\x1b[33m[WARN]\x1b[0m"
	case levelFail:
		return "\x1b[31m[FAIL]\x1b[0m"
	}
	return "[????]"
}

func checkVersion(current string) (level, string) {
	if current == "" || current == "dev" {
		return levelWarn, "dev build — version check skipped"
	}
	// The channel this build follows answers, the same way `reminal upgrade`
	// asks it: a build on another line of releases is compared against its
	// own feed, not the public one.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tag, err := updater.LatestTag(ctx)
	if err != nil {
		return levelWarn, fmt.Sprintf("v%s (couldn't check for updates: %v)", current, err)
	}
	if tag == "" || !updater.Newer(current, tag) {
		return levelOK, fmt.Sprintf("v%s (latest)", current)
	}
	return levelWarn, fmt.Sprintf("v%s — newer available: %s (run `reminal upgrade`)", current, tag)
}

func checkRelay() (level, string) {
	// Probe the web URL (https) since /ws is a WebSocket upgrade endpoint and
	// can't be reached with a plain GET; both share the same Cloudflare host.
	url := config.WebURL()
	if url == "" {
		return levelFail, "no relay configured (REMINAL_RELAY/REMINAL_WEB unset)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return levelFail, fmt.Sprintf("%s — bad URL: %v", url, err)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return levelFail, fmt.Sprintf("%s — unreachable: %v", url, err)
	}
	resp.Body.Close()
	elapsed := time.Since(start).Round(time.Millisecond)
	if resp.StatusCode >= 500 {
		return levelFail, fmt.Sprintf("%s — relay returned %s (%v)", url, resp.Status, elapsed)
	}
	return levelOK, fmt.Sprintf("%s — reachable, %v", url, elapsed)
}

// checkRelayWS confirms the relay accepts a WebSocket upgrade. Many corporate
// proxies pass plain HTTPS but strip the Upgrade header (or sit behind a
// load balancer that does), which would make checkRelay green but reminal
// hang at "Connecting…" forever. We dial a dummy session ID and accept any
// outcome other than a transport-layer failure as proof that WS works —
// the relay is expected to immediately close with "session not found" since
// AAAAAAAA isn't a real session.
func checkRelayWS() (level, string) {
	url := config.SessionWS("AAAAAAAA", string(protocol.RoleViewer))
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 3 * time.Second
	start := time.Now()
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return levelFail, fmt.Sprintf("%s — upgrade failed: %v", url, err)
	}
	elapsed := time.Since(start).Round(time.Millisecond)
	_ = conn.Close()
	return levelOK, fmt.Sprintf("upgrade OK (%v) — relay accepts WS connections", elapsed)
}

func checkTerminal() (level, string) {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return levelWarn, "stdout is not a TTY (running in a pipe?)"
	}
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return levelWarn, fmt.Sprintf("can't read terminal size: %v", err)
	}
	t := os.Getenv("TERM")
	if t == "" {
		t = "(TERM unset)"
	}
	return levelOK, fmt.Sprintf("%s, %dx%d", t, cols, rows)
}

func checkShell() (level, string) {
	sh := config.Shell()
	if _, err := os.Stat(sh); err != nil {
		return levelFail, fmt.Sprintf("%s not found or unreadable", sh)
	}
	return levelOK, sh
}

func checkActiveSession() (level, string) {
	a, err := session.ReadActive()
	if errors.Is(err, os.ErrNotExist) {
		return levelOK, "none (run `reminal` to start one)"
	}
	if err != nil {
		return levelWarn, fmt.Sprintf("couldn't read active record: %v", err)
	}
	return levelOK, fmt.Sprintf("%s (PID %d, started %s)", a.ID, a.PID, a.StartedAt.Format(time.RFC3339))
}

func checkConfigDir() (level, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return levelFail, fmt.Sprintf("can't find home dir: %v", err)
	}
	dir := filepath.Join(home, ".reminal")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return levelFail, fmt.Sprintf("%s not writable: %v", dir, err)
	}
	// Round-trip a sentinel file to confirm we can actually write.
	tmp, err := os.CreateTemp(dir, ".doctor-*")
	if err != nil {
		return levelFail, fmt.Sprintf("%s not writable: %v", dir, err)
	}
	name := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(name)
	return levelOK, fmt.Sprintf("%s writable", dir)
}

// checkAtRestKey: the key everything saved is sealed with. Missing from its
// store while sessions run is damage that running processes can repair.
func checkAtRestKey() (level, string) {
	ks := atrest.KeyState()
	switch {
	case ks.Source == "":
		return levelOK, "none yet (made at the first save)"
	case ks.Missing:
		return levelFail, "the at-rest key this machine saves with is missing from disk; running sessions still hold it — restart nothing; run `reminal doctor --repair-key`"
	case ks.MaybeMissing:
		return levelWarn, "the " + ks.Source + " is locked or no longer has the key this machine saves with; if it stays this way with sessions running, `reminal doctor --repair-key` puts it back from one of them — restart nothing"
	case ks.MetaDamaged:
		return levelFail, "~/.reminal/atrest.json is damaged; saved sessions cannot be opened until it is restored from a backup"
	case ks.FileOnDesktop:
		return levelWarn, "kept in a file in ~/.reminal although this is a desktop login (made outside the keystore, over SSH or by a test); it moves into the keystore at the next save once that answers"
	}
	return levelOK, "in place (" + ks.Source + ")"
}

// RepairAtRestKey asks every running session on this machine for the at-rest
// key over its control socket and writes it back into the store atrest.json
// names. Nothing is restarted and nothing is minted.
func RepairAtRestKey() error {
	ks := atrest.KeyState()
	if ks.MetaDamaged {
		return errors.New("~/.reminal/atrest.json is damaged, so there is nothing to say which key is current; restore it from a backup")
	}
	if ks.Source == "" {
		return errors.New("no at-rest key is recorded here (atrest.json missing); nothing to repair")
	}
	if !ks.MissingKeyLikely() {
		fmt.Println("  The at-rest key is in place; nothing to repair.")
		return nil
	}
	dir, err := reminalDir()
	if err != nil {
		return err
	}
	socks, _ := filepath.Glob(filepath.Join(dir, "agent-*.sock"))
	asked := 0
	for _, s := range socks {
		var pid int
		if _, err := fmt.Sscanf(filepath.Base(s), "agent-%d.sock", &pid); err != nil {
			continue
		}
		asked++
		hexKey, err := sendControlToDeadline(pid, "atrest-key", 2*time.Second)
		if err != nil || hexKey == "" {
			continue
		}
		if err := atrest.RestoreKey(hexKey); err != nil {
			fmt.Printf("  session %d offered a key that was not accepted: %v\n", pid, err)
			continue
		}
		fmt.Printf("  Recovered the at-rest key from a running session (pid %d) and wrote it back.\n", pid)
		return nil
	}
	if asked == 0 {
		return errors.New("no running session to ask; the key is lost unless you have a backup of ~/.reminal/atrest.key")
	}
	return fmt.Errorf("asked %d running session(s); none held the key atrest.json names", asked)
}

// checkWindowNotes: how many notes are kept, and where.
func checkWindowNotes() (level, string) {
	n, p := NotesCount()
	if p == "" || n == 0 {
		return levelOK, "none"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && strings.HasPrefix(p, home) {
		p = "~" + strings.TrimPrefix(p, home)
	}
	return levelOK, fmt.Sprintf("%d kept in %s (survive daemon restarts)", n, p)
}
