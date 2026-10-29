// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reminal/internal/atomicfile"
)

// pluginFile is an agent's hooks when they are a plugin it loads rather than
// entries in its config (OpenCode): one file reminal writes, which reports
// the session's attention state and its conversation as hooks do, through
// `reminal hook`. Removed only while it is still reminal's (pluginMark).
type pluginFile struct {
	path string                  // relative to $HOME
	body func(exe string) string // the plugin, calling reminal at exe
}

// pluginMark opens every plugin reminal writes: a file without it is the
// person's own, never overwritten or removed.
const pluginMark = "// reminal's plugin, written by `reminal integrate`"

func applyPlugin(p *pluginFile, home, exe string, remove bool) error {
	path := filepath.Join(home, p.path)
	old, err := os.ReadFile(path)
	switch {
	case err == nil && !strings.HasPrefix(string(old), pluginMark):
		return errors.New(p.path + " is not reminal's: left as it is")
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	case remove:
		if err != nil {
			return nil
		}
		return os.Remove(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(p.body(exe)), 0o644)
}

// opencodePlugin reports what OpenCode's own conversation is doing (not its
// subagents', which run as sessions of their own with a parent) and which
// conversation it is: busy is a turn, idle its end, a permission asked
// waits on the person. Only in a reminal session.
func opencodePlugin(exe string) string {
	bin, _ := json.Marshal(exe)
	return pluginMark + `: what this OpenCode is doing,
// and which conversation it is in, for reminal's list and to resume it.
import { spawn } from "node:child_process";

const BIN = ` + string(bin) + `;

export const Reminal = async () => {
	if (!process.env.REMINAL_SESSION) return {};
	let conv = "";
	const children = new Set();
	const report = (state) => {
		try {
			const p = spawn(BIN, ["hook", state], { stdio: ["pipe", "ignore", "ignore"], detached: true });
			p.on("error", () => {});
			p.stdin.on("error", () => {});
			p.stdin.end(conv ? JSON.stringify({ session_id: conv }) : "");
			p.unref();
		} catch {
			// no reminal to tell: OpenCode carries on
		}
	};
	return {
		event: async ({ event }) => {
			const props = event.properties ?? {};
			if (event.type === "session.created" && props.info?.parentID) children.add(props.info.id);
			const id = props.sessionID;
			if (!id || children.has(id)) return;
			switch (event.type) {
				case "session.status":
					conv = id;
					report(props.status?.type === "idle" ? "done" : "working");
					break;
				case "session.idle":
					conv = id;
					report("done");
					break;
				case "permission.asked":
				case "permission.updated":
					report("input");
					break;
				case "permission.replied":
					report("working");
					break;
			}
		},
	};
};
`
}
