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

// claudeHelp is the head of Claude Code's own --help: its usage and the
// options a restore reads off it (which take a value, which do not).
const claudeHelp = `Usage: claude [options] [command] [prompt]

Claude Code - starts an interactive session by default, use -p/--print for
non-interactive output

Arguments:
  prompt                                  Your prompt

Options:
  -d, --debug [filter]                    Enable debug mode
  --verbose                               Override verbose mode setting from config
  -p, --print                             Print response and exit (useful for pipes)
  --dangerously-skip-permissions          Bypass all permission checks
  --model <model>                         Model for the current session
  --append-system-prompt <prompt>         Append a system prompt to the default system prompt
  -c, --continue                          Continue the most recent conversation
  -r, --resume [value]                    Resume a conversation by session ID
  --session-id <uuid>                     Use a specific session ID for the conversation
  -v, --version                           Output the version number
  -h, --help                              Display help for command
`

// answerAsk answers what reminal asks claude without a session of it — its
// version (the hook heal asks), its help (a restore reads which flags take a
// value), its MCP servers (integrate adds reminal's: "claude mcp add …") —
// as Claude Code does, each written to asks.log with its pid before it is
// answered, and says whether args were such an ask: then it exits, no
// session started (check.sh holds starts.log to it).
func answerAsk(dir string, args []string) bool {
	answer, ask := "", false
	if len(args) > 0 && args[0] == "mcp" {
		ask = true
		if len(args) > 1 && args[1] == "add" {
			answer = "Added MCP server to local config\n"
		} else if len(args) > 1 && args[1] == "remove" {
			answer = "Removed MCP server from local config\n"
		}
	}
	for _, a := range args {
		if ask {
			break
		}
		switch a {
		case "--version", "-v":
			answer, ask = "2.1.296 (Claude Code)\n", true
		case "--help", "-h":
			answer, ask = claudeHelp, true
		}
	}
	if !ask {
		return false
	}
	if f, err := os.OpenFile(filepath.Join(dir, "asks.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(f, "ask: pid %d %s\n", os.Getpid(), strings.Join(args, " "))
		f.Close()
	}
	fmt.Print(answer)
	return true
}

func main() {
	home0, _ := os.UserHomeDir()
	dir0 := filepath.Join(home0, ".fake-claude")
	_ = os.MkdirAll(dir0, 0o700)
	if answerAsk(dir0, os.Args[1:]) {
		return
	}
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".fake-claude")
	_ = os.MkdirAll(dir, 0o700)
	if f, err := os.OpenFile(filepath.Join(dir, "argv.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, "argv: "+strings.Join(os.Args[1:], " "))
		f.Close()
	}
	// Each session started, with its pid: no ask's (answerAsk) is ever one.
	if f, err := os.OpenFile(filepath.Join(dir, "starts.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintf(f, "start: pid %d %s\n", os.Getpid(), strings.Join(os.Args[1:], " "))
		f.Close()
	}
	conv, resume, cont, prompt := "", false, false, ""
	for i := 1; i < len(os.Args); i++ {
		switch a := os.Args[i]; {
		case a == "--resume":
			if i+1 < len(os.Args) {
				conv, resume = os.Args[i+1], true
				i++
			}
		case a == "--continue":
			conv, cont = latest(dir), true
		case !strings.HasPrefix(a, "-") && !strings.HasPrefix(os.Args[i-1], "-"):
			prompt = a // a word not a flag's value: its first message
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
		// A turn's end says what it left running in the background, as
		// claude's Stop does (not with FAKE_CLAUDE_BG_SAID=0, as an older
		// build that does not).
		bg := ""
		if event == "Stop" && os.Getenv("FAKE_CLAUDE_BG_SAID") != "0" {
			bg = `,"background_tasks":[]`
		}
		c := exec.Command("reminal", "hook", state)
		c.Stdin = strings.NewReader(fmt.Sprintf(`{"session_id":%q,"hook_event_name":%q%s}`, id, event, bg))
		_ = c.Run()
	}
	hook := func(state, event string) { hookAs(state, event, conv) }
	// Like claude, it fires no Stop as it starts: its first rest is read off
	// its screen, or comes with its first turn's end. FAKE_CLAUDE_START_STOP=1
	// says "at rest" at once (naming no conversation: none ended a turn).
	if os.Getenv("FAKE_CLAUDE_START_STOP") == "1" {
		hookAs("done", "Stop", "")
	}
	turn := func(text string) {
		hook("working", "UserPromptSubmit")
		if text == "bg" {
			// A command left going in the background through a shell, its
			// hook none the wiser.
			_ = exec.Command("sh", "-c", "sleep 633; true").Start()
		}
		if text == "orphan" {
			// One started with & inside a command, which outlives it: no
			// longer under the agent, still on its terminal.
			_ = exec.Command("sh", "-c", "(sleep 644 &); true").Run()
		}
		line := "you said: " + text
		fmt.Println(line)
		if f, err := os.OpenFile(path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
		// Where Claude Code keeps it, too, which reminal reads to know a
		// conversation is still there: ~/.claude/projects/<cwd, / as ->/<id>.jsonl.
		if cwd, err := os.Getwd(); err == nil {
			proj := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
			_ = os.MkdirAll(proj, 0o700)
			if f, err := os.OpenFile(filepath.Join(proj, conv+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
				fmt.Fprintf(f, "{\"type\":\"user\",\"text\":%q}\n", text)
				f.Close()
			}
		}
		hook("done", "Stop")
	}
	// A first message on its command line (claude "do X") is its first
	// turn, begun before it ever rests.
	if prompt != "" {
		turn(prompt)
	}
	in := bufio.NewScanner(os.Stdin)
	for fmt.Print("claude> "); in.Scan(); fmt.Print("claude> ") {
		turn(in.Text())
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
