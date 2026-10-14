// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"reminal/internal/session"
)

// What resuming a coding agent's conversation costs, and how a restore
// keeps that from hurting the session it comes back in.
//
// claude --resume reads the whole conversation before it draws its prompt:
// CPU-bound, and its memory peaks at about 260 MB plus twice the
// conversation's file (measured on 2.1.296: 300 MB peaked at ~850 MB). On a
// small machine a big one could take the machine's memory, and the session
// with it.

const (
	resumeBaseBytes = 260 << 20 // claude's own, before the conversation
	resumeHeadroom  = 300 << 20 // left for the session, its shell and reminal
)

// resumeTooBig says why r's conversation is not resumed on this machine
// ("" when it is): claude's, its file known, needing more memory than the
// machine has to spare. Never a guess: a size or a memory not known resumes.
func resumeTooBig(r session.Restore) string {
	if r.Fg != "claude" || !session.ValidConv(r.Conv) {
		return ""
	}
	size := claudeConvSize(r.Conv)
	mem := memTotal()
	if size <= 0 || mem <= 0 {
		return ""
	}
	need := resumeBaseBytes + 2*size
	if need <= mem-resumeHeadroom {
		return ""
	}
	return fmt.Sprintf("this conversation's file is %d MB; resuming it takes about %d MB of memory, more than this machine's %d MB can spare, so it was not resumed — to try: claude --resume %s, or start a new conversation",
		size>>20, need>>20, mem>>20, r.Conv)
}

// claudeConvSize is the size of claude's file for conversation conv, or 0:
// ~/.claude/projects/<its folder>/<conv>.jsonl, the largest if two folders
// have one. conv is a ValidConv id, so nothing in it is read as a pattern.
func claudeConvSize(conv string) int64 {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", conv+".jsonl"))
	var max int64
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.Mode().IsRegular() && fi.Size() > max {
			max = fi.Size()
		}
	}
	return max
}

// memTotal is the memory this session may use, in bytes: the machine's
// (Linux: /proc/meminfo), or its cgroup's limit when that is lower (inside
// a container MemTotal is the host's); 0 where neither is known.
var memTotal = func() int64 {
	m := meminfoTotal()
	if c := cgroupMemoryLimit(); c > 0 && (m == 0 || c < m) {
		m = c
	}
	return m
}

func meminfoTotal() int64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "MemTotal:"); ok {
			kb, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(v), " kB"), 10, 64)
			if err == nil {
				return kb << 10
			}
		}
	}
	return 0
}

// cgroupMemoryLimit is this process's cgroup memory limit (v2 memory.max,
// or v1 memory.limit_in_bytes), or 0 for none ("max", or v1's near-2^63
// "unlimited").
func cgroupMemoryLimit() int64 {
	for _, p := range []string{"/sys/fs/cgroup/memory.max", "/sys/fs/cgroup/memory/memory.limit_in_bytes"} {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		return parseMemLimit(string(b))
	}
	return 0
}

func parseMemLimit(s string) int64 {
	s = strings.TrimSpace(s)
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || n >= 1<<62 {
		return 0
	}
	return n
}

// lowPriority runs argv under nice, in a POSIX shell where there is one:
// the conversation is read behind the session, never ahead of what its
// person types or of reminal serving them.
func lowPriority(argv []string, shell string) []string {
	if runtime.GOOS == "windows" || isCmdShell(shell) {
		return argv
	}
	base := strings.ToLower(shell[strings.LastIndexAny(shell, `/\`)+1:])
	if strings.HasPrefix(base, "pwsh") || strings.HasPrefix(base, "powershell") {
		return argv
	}
	if _, err := lookNice("nice"); err != nil {
		return argv
	}
	return append([]string{"nice", "-n", "10"}, argv...)
}

// lookNice is exec.LookPath; a test stands in for it.
var lookNice = exec.LookPath
