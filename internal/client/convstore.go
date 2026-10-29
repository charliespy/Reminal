// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"path/filepath"

	"reminal/internal/session"
)

// convStores: for each coding agent that sleeps, where it keeps the file of
// a conversation, as a pattern for its id. A sleep and a wake check the file
// is there, so it never sleeps on, or wakes to, a conversation it could not
// open; resuming a big one is weighed by its size (resumeTooBig).
var convStores = map[string]func(conv string) string{
	// <its folder>/projects/<project>/<conv>.jsonl
	"claude": func(conv string) string { return underDir(claudeConfigDir(), "projects", "*", conv+".jsonl") },
	// <CODEX_HOME, else ~/.codex>/sessions/<year>/<month>/<day>/rollout-<when>-<conv>.jsonl
	"codex": func(conv string) string {
		return underDir(codexHome(), "sessions", "*", "*", "*", "rollout-*-"+conv+".jsonl")
	},
	// ~/.qwen/projects/<project>/chats/<conv>.jsonl
	"qwen": func(conv string) string { return underHome(".qwen", "projects", "*", "chats", conv+".jsonl") },
}

// convSize is the size of agent's file for conversation conv, the largest
// if two projects have one; 0 when none, or agent's are not known. conv
// must be a ValidConv id, so nothing in it is read as a pattern.
func convSize(agent, conv string) int64 {
	where, ok := convStores[agent]
	if !ok || !session.ValidConv(conv) {
		return 0
	}
	pattern := where(conv)
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

// codexHome is where Codex keeps its files: CODEX_HOME when set, else
// ~/.codex.
func codexHome() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir
	}
	return underHome(".codex")
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
