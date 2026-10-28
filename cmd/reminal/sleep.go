// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"reminal/internal/client"
	"reminal/internal/proc"
	"reminal/internal/session"
)

// `reminal sleep <id|name> [--anyway]`: end a session's processes the way a
// machine shutdown does, its record kept, so it frees its memory and wakes
// as itself (client/sleep.go). It refuses when a sleep would end something
// a wake can't bring back, and says what; --anyway lets that go, and the
// list then shows what was let go of.
func runSleep(args []string) error {
	idArg, anyway := "", false
	for _, a := range args {
		switch {
		case a == "--anyway":
			anyway = true
		case !strings.HasPrefix(a, "-") && idArg == "":
			idArg = a
		}
	}
	if idArg != "" {
		if r := findSleeper(idArg); r != nil {
			fmt.Printf("  %s is already asleep · reminal wake %s\n", r.ID, r.ID)
			return nil
		}
	}
	a, err := resolveActive(idArg)
	if err != nil {
		return err
	}
	cmd := "sleep"
	if anyway {
		cmd = "sleep anyway"
	}
	if _, err := sendControl(a.PID, cmd, 20*time.Second); err != nil {
		msg := strings.TrimPrefix(err.Error(), "agent: ")
		if rest, ok := strings.CutPrefix(msg, "lose:"); ok {
			return fmt.Errorf("%s\n  reminal sleep %s --anyway   to sleep it all the same", strings.TrimSpace(rest), a.ID)
		}
		return errors.New(msg)
	}
	// It said yes and is ending itself; wait for it to be gone.
	deadline := time.Now().Add(10 * time.Second)
	for proc.Alive(a.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if proc.Alive(a.PID) {
		return fmt.Errorf("%s said it would sleep but is still running (PID %d)", a.ID, a.PID)
	}
	fmt.Printf("  %s %s asleep · reminal wake %s\n", cGreen("✓"), cBold(a.ID), a.ID)
	return nil
}

// `reminal wake <id|name>`: bring a sleeping session back, on its id, PIN
// and conversation.
func runWake(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: reminal wake <id|name>   (reminal list shows who is asleep)")
	}
	r := findSleeper(args[0])
	if r == nil {
		if client.SessionRunning(strings.ToUpper(args[0])) {
			fmt.Printf("  %s is awake\n", strings.ToUpper(args[0]))
			return nil
		}
		return fmt.Errorf("no session asleep matches %q (reminal list)", args[0])
	}
	if _, err := client.WakeSession(*r); err != nil {
		return err
	}
	fmt.Printf("  %s %s awake · same link and PIN\n", cGreen("✓"), cBold(r.ID))
	return nil
}

// `reminal keep-awake <id|name> [--off]`: a session that must not be put to
// sleep (or, --off, may be again).
func runKeepAwake(args []string) error {
	idArg, off := "", false
	for _, a := range args {
		switch {
		case a == "--off":
			off = true
		case !strings.HasPrefix(a, "-") && idArg == "":
			idArg = a
		}
	}
	id := ""
	if r := findSleeper(idArg); r != nil && idArg != "" {
		id = r.ID
	} else {
		a, err := resolveActive(idArg)
		if err != nil {
			return err
		}
		id = a.ID
	}
	if err := session.SetAwakePin(id, !off); err != nil {
		return err
	}
	if off {
		fmt.Printf("  %s may sleep again\n", id)
	} else {
		fmt.Printf("  %s is kept awake · reminal keep-awake %s --off to undo\n", id, id)
	}
	return nil
}

// sleepers are the sessions asleep, newest sleep first.
func sleepers() []session.Restore {
	all, _ := session.ReadRestores()
	var out []session.Restore
	for i := len(all) - 1; i >= 0; i-- {
		if session.IsAsleep(all[i].ID) && !client.SessionRunning(all[i].ID) {
			out = append(out, all[i])
		}
	}
	return out
}

// findSleeper is the sleeping session arg names (id, name or id prefix), or
// nil when none, or more than one, does.
func findSleeper(arg string) *session.Restore {
	q := strings.ToLower(strings.TrimSpace(arg))
	if q == "" {
		return nil
	}
	var hit *session.Restore
	for _, r := range sleepers() {
		if strings.ToLower(r.ID) == q || strings.ToLower(r.Name) == q || strings.HasPrefix(strings.ToLower(r.ID), q) {
			if hit != nil {
				return nil
			}
			r := r
			hit = &r
		}
	}
	return hit
}

// printSleepers lists the sessions asleep that match filter ("" for all),
// under the running ones; how many it printed.
func printSleepers(filter string) int {
	n := 0
	for _, r := range sleepers() {
		if filter != "" && !strings.Contains(strings.ToLower(r.ID+" "+r.Name+" "+r.Cwd), filter) {
			continue
		}
		if n == 0 {
			fmt.Println(cDim("  asleep:"))
		}
		n++
		name := r.Name
		if name == "" {
			name = cDim("—")
		}
		line := "asleep · wakes with reminal wake " + r.ID
		if a := session.ReadAsleep(r.ID); a != nil {
			line = "asleep " + restoreAgo(time.Since(a.SleptAt)) + " ago · reminal wake " + r.ID
			if len(a.Dropped) > 0 {
				line += " · let go of: " + strings.Join(a.Dropped, "; ")
			}
		}
		fmt.Printf("  %s  %s  %s\n", name, cBold(r.ID), cDim(line))
	}
	if n > 0 {
		fmt.Println()
	}
	return n
}
