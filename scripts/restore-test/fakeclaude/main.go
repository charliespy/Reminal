// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// fakeclaude stands in for claude in the restore rig: a native program named
// claude (as the real one is — a script would show as its interpreter on
// macOS), whose conversations live in ~/.fake-claude/<id>.conv, resumed with
// --resume <id> or --continue, and which — like claude's own hooks — reports
// its conversation id through `reminal hook` on every turn (not with
// FAKE_CLAUDE_HOOKS=0, as for someone without reminal's hooks installed).
// Like claude, it keeps ~/.claude/sessions/<pid>.json naming the
// conversation the process is in. `--resume` alone stands for claude's list;
// it starts a new conversation.
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".fake-claude")
	_ = os.MkdirAll(dir, 0o700)
	if f, err := os.OpenFile(filepath.Join(dir, "argv.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, "argv: "+strings.Join(os.Args[1:], " "))
		f.Close()
	}
	conv, resume, cont := "", false, false
	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--resume":
			if i+1 < len(os.Args) {
				conv, resume = os.Args[i+1], true
				i++
			}
		case "--continue":
			conv, cont = latest(dir), true
		}
	}
	path := func() string { return filepath.Join(dir, conv+".conv") }
	// As Claude Code: a conversation is written on its first message, and
	// one never written cannot be resumed — it says so and exits.
	if _, err := os.Stat(path()); resume && err != nil {
		fmt.Println("No conversation found with session ID: " + conv)
		os.Exit(1)
	}
	if cont && conv == "" {
		fmt.Println("No conversation found to continue")
		os.Exit(1)
	}
	if b, err := os.ReadFile(path()); conv != "" && err == nil {
		fmt.Println("fake-claude: resumed conversation " + conv)
		for _, l := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
			if l != "" {
				fmt.Println("  (earlier) " + l)
			}
		}
	} else {
		b := make([]byte, 6)
		_, _ = rand.Read(b)
		conv = "conv-" + hex.EncodeToString(b)
		fmt.Println("fake-claude: new conversation " + conv) // written on its first message
	}
	sess := filepath.Join(home, ".claude", "sessions")
	_ = os.MkdirAll(sess, 0o700)
	_ = os.WriteFile(filepath.Join(sess, strconv.Itoa(os.Getpid())+".json"),
		[]byte(fmt.Sprintf(`{"pid":%d,"sessionId":%q,"kind":"interactive"}`, os.Getpid(), conv)), 0o600)
	hooks := os.Getenv("FAKE_CLAUDE_HOOKS") != "0"
	hookAs := func(state, event, id string) {
		if !hooks {
			return
		}
		c := exec.Command("reminal", "hook", state)
		c.Stdin = strings.NewReader(fmt.Sprintf(`{"session_id":%q,"hook_event_name":%q}`, id, event))
		_ = c.Run()
	}
	hook := func(state, event string) { hookAs(state, event, conv) }
	// Its first word: at rest. The rig's own, not claude's (which fires no
	// Stop as it starts): so it names no conversation, as none ended a turn.
	hookAs("done", "Stop", "")
	in := bufio.NewScanner(os.Stdin)
	for fmt.Print("claude> "); in.Scan(); fmt.Print("claude> ") {
		hook("working", "UserPromptSubmit")
		line := "you said: " + in.Text()
		fmt.Println(line)
		if f, err := os.OpenFile(path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
		hook("done", "Stop")
	}
}

// latest is the most recently written conversation, as --continue picks it.
func latest(dir string) string {
	m, _ := filepath.Glob(filepath.Join(dir, "*.conv"))
	sort.Slice(m, func(i, j int) bool {
		a, _ := os.Stat(m[i])
		b, _ := os.Stat(m[j])
		return a.ModTime().After(b.ModTime())
	})
	if len(m) == 0 {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(m[0]), ".conv")
}
