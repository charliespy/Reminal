// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"reminal/internal/session"
)

// convStore is where a coding agent keeps its conversations (resumer.store), for a sleep
// and a wake to check the one it slept in is there: so it never sleeps on,
// or wakes to, a conversation it could not open.
type convStore struct {
	// file is a pattern for the file conversation conv is kept in; its size
	// also weighs resuming it (resumeTooBig).
	file func(conv string) string
	// has asks the agent itself, for one that keeps them where no file names
	// them (a database): whether it has conv, and whether it could say.
	has func(conv, cwd string) (there, known bool)
}

// known says the store can be looked in: an agent with one sleeps.
func (s convStore) known() bool { return s.file != nil || s.has != nil }

// claudeConvFile: <its folder>/projects/<project>/<conv>.jsonl
func claudeConvFile(conv string) string {
	return underDir(claudeConfigDir(), "projects", "*", conv+".jsonl")
}

// codexConvFile: <CODEX_HOME, else ~/.codex>/sessions/<year>/<month>/<day>/rollout-<when>-<conv>.jsonl
func codexConvFile(conv string) string {
	return underDir(codexHome(), "sessions", "*", "*", "*", "rollout-*-"+conv+".jsonl")
}

// piConvFile: <PI_CODING_AGENT_DIR, else ~/.pi/agent>/sessions/<project>/<when>_<conv>.jsonl
func piConvFile(conv string) string {
	return underDir(piAgentDir(), "sessions", "*", "*_"+conv+".jsonl")
}

// qwenConvFile: ~/.qwen/projects/<project>/chats/<conv>.jsonl
func qwenConvFile(conv string) string {
	return underHome(".qwen", "projects", "*", "chats", conv+".jsonl")
}

// convSize is the size of agent's file for conversation conv, the largest
// if two projects have one; 0 when none, or agent keeps no files. conv must
// be a ValidConv id, so nothing in it is read as a pattern.
func convSize(agent, conv string) int64 {
	s := resumers[agent].store
	if s.file == nil || !session.ValidConv(conv) {
		return 0
	}
	pattern := s.file(conv)
	if pattern == "" {
		return 0
	}
	matches, _ := filepath.Glob(pattern)
	var max int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.Mode().IsRegular() && fi.Size() > max {
			max = fi.Size()
		}
	}
	return max
}

// convThere says agent has conversation conv, last open in cwd, and
// whether that could be told (known false: no file pattern, or the agent
// could not be asked).
func convThere(agent, conv, cwd string) (there, known bool) {
	s := resumers[agent].store
	switch {
	case !session.ValidConv(conv):
		return false, s.known()
	case s.file != nil:
		return convSize(agent, conv) > 0, true
	case s.has != nil:
		return s.has(conv, cwd)
	}
	return false, false
}

// convAskWait is how long an agent is given to list its conversations.
const convAskWait = 15 * time.Second

// opencodeHas asks opencode, in cwd (it lists a project's sessions), for
// its sessions, as JSON, and looks for conv among them.
func opencodeHas(conv, cwd string) (there, known bool) {
	bin, err := exec.LookPath("opencode")
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), convAskWait)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "session", "list", "--format", "json")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return false, false
	}
	var list []struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(out, &list) != nil {
		return false, false
	}
	for _, s := range list {
		if s.ID == conv {
			return true, true
		}
	}
	return false, true
}

// codexHome is where Codex keeps its files: CODEX_HOME when set, else
// ~/.codex.
func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return underHome(".codex")
}

// piAgentDir is where pi keeps its files: PI_CODING_AGENT_DIR when set,
// else ~/.pi/agent.
func piAgentDir() string {
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return dir
	}
	return underHome(".pi", "agent")
}

// underDir is dir joined with parts, or "" when dir is not known.
func underDir(dir string, parts ...string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(append([]string{dir}, parts...)...)
}

// underHome is underDir of the home folder.
func underHome(parts ...string) string {
	home, _ := os.UserHomeDir()
	return underDir(home, parts...)
}
