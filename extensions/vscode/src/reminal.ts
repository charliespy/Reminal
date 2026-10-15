// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// The parts that talk to the reminal CLI, kept free of the vscode module so
// they can be tested with plain node.

import { execFile } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

export const INSTALL_URL = "https://reminal.app";
// The same lines reminal.app and the web viewer's share text give.
export const INSTALL_UNIX = "curl -fsSL https://reminal.app/install.sh | sh";
export const INSTALL_WINDOWS = "irm https://reminal.app/install.ps1 | iex";

// One entry of `reminal info --all --json`.
export interface SessionInfo {
  id: string;
  name?: string;
  pin: string;
  open_url: string;
  join_url: string;
  cwd?: string;
  pid: number;
  started_at: string;
  // The attention state `reminal list` shows. Absent for a bare shell, and
  // from a reminal older than this field.
  attn?: string;
  attn_since?: string;
}

export type Status = "needs" | "working" | "done";

// statusOf maps a session's attn to the three states the viewer shows.
// Anything else (a bare shell, an older reminal, a value added later) is no
// status at all rather than a guess.
export function statusOf(attn: string | undefined): Status | undefined {
  switch (attn) {
    case "input":
    case "logged-out":
      return "needs";
    case "working":
      return "working";
    case "done":
      return "done";
  }
  return undefined;
}

export const STATUS_LABEL: Record<Status, string> = {
  needs: "Needs you",
  working: "Working",
  done: "Done",
};

// summary is the status bar's text for a set of terminal states: the counts
// that matter, most urgent first, or just how many reminal terminals there are
// when none of them reports a state.
export function summary(states: (Status | undefined)[]): { text: string; top?: Status } {
  const n = (s: Status) => states.filter((x) => x === s).length;
  const parts: string[] = [];
  if (n("needs")) parts.push(`${n("needs")} needs you`);
  if (n("working")) parts.push(`${n("working")} working`);
  if (n("done")) parts.push(`${n("done")} done`);
  const top: Status | undefined = n("needs") ? "needs" : n("working") ? "working" : n("done") ? "done" : undefined;
  const icon = { needs: "$(bell)", working: "$(sync~spin)", done: "$(check)" }[top ?? "done"];
  if (!parts.length) return { text: `$(cloud) reminal: ${states.length}` };
  return { text: `${icon} reminal: ${parts.join(", ")}`, top };
}

function isExecutable(p: string): boolean {
  try {
    const st = fs.statSync(p);
    if (!st.isFile()) return false;
    if (process.platform === "win32") return true;
    fs.accessSync(p, fs.constants.X_OK);
    return true;
  } catch {
    return false;
  }
}

// findReminal returns the reminal program to run, or undefined when there is
// none. An explicit setting wins; then PATH; then the folders install.sh and
// install.ps1 put it in, because an editor started from the Dock or Start menu
// often has a PATH without them.
export function findReminal(
  configured: string,
  env: NodeJS.ProcessEnv = process.env,
  platform: NodeJS.Platform = process.platform,
  home: string = os.homedir(),
): string | undefined {
  const exe = platform === "win32" ? "reminal.exe" : "reminal";
  const configuredPath = configured.trim().replace(/^~(?=$|[\\/])/, home);
  // Only absolute paths: a relative one would mean one file to the check
  // here and another to the terminal, which resolves it from its own folder.
  if (configuredPath) return path.isAbsolute(configuredPath) && isExecutable(configuredPath) ? configuredPath : undefined;
  const sep = platform === "win32" ? ";" : ":";
  const dirs = (env.PATH ?? env.Path ?? "").split(sep).filter((d) => path.isAbsolute(d));
  if (platform === "win32") {
    if (env.LOCALAPPDATA) dirs.push(path.join(env.LOCALAPPDATA, "Programs", "reminal"));
  } else {
    dirs.push(path.join(home, ".local", "bin"), "/usr/local/bin", "/opt/homebrew/bin");
  }
  for (const d of dirs) {
    const p = path.join(d, exe);
    if (isExecutable(p)) return p;
  }
  return undefined;
}

// cleanEnv is the environment for a new reminal: without the variables of a
// reminal session the editor itself may have been started from, so the new
// terminal is not mistaken for being inside that one.
export const CLEAN_ENV: { [k: string]: null } = {
  REMINAL_SESSION: null,
  REMINAL_SESSION_PIN: null,
  REMINAL_SESSION_URL: null,
};

function processEnv(): NodeJS.ProcessEnv {
  const env = { ...process.env };
  for (const k of Object.keys(CLEAN_ENV)) delete env[k];
  return env;
}

// querySessions is every live session on this machine, or undefined when
// reminal couldn't be asked (it failed, or timed out).
export function querySessions(bin: string): Promise<SessionInfo[] | undefined> {
  return new Promise((resolve) => {
    execFile(bin, ["info", "--all", "--json"], { env: processEnv(), timeout: 5000, windowsHide: true }, (err, stdout) => {
      if (err) return resolve(undefined);
      const text = stdout.trim();
      if (!text.startsWith("[")) return resolve(/^No reminal/.test(text) ? [] : undefined);
      try {
        JSON.parse(text);
      } catch {
        return resolve(undefined);
      }
      resolve(parseSessions(text));
    });
  });
}

export async function listSessions(bin: string): Promise<SessionInfo[]> {
  return (await querySessions(bin)) ?? [];
}

export function parseSessions(stdout: string): SessionInfo[] {
  const text = stdout.trim();
  if (!text.startsWith("[")) return []; // "No reminal shell sessions running."
  try {
    const v = JSON.parse(text);
    return Array.isArray(v) ? v.filter((s) => s && typeof s.id === "string") : [];
  } catch {
    return [];
  }
}

// matchSession picks the session a terminal is running. The terminal's process
// is reminal itself, so its pid is the session's pid. Only when the pid is
// unknown is the newest session in the same folder that started after the
// terminal did taken instead: with a pid, a miss means "not registered yet",
// and guessing then could pick a neighbour started a moment earlier.
export function matchSession(
  sessions: SessionInfo[],
  pid: number | undefined,
  cwd: string | undefined,
  since: number,
): SessionInfo | undefined {
  if (pid) return sessions.find((s) => s.pid === pid);
  const norm = (p: string) => path.resolve(p).replace(/[\\/]+$/, "").toLowerCase();
  const fresh = sessions
    .filter((s) => Date.parse(s.started_at) >= since - 2000)
    .filter((s) => !cwd || !s.cwd || norm(s.cwd) === norm(cwd))
    .sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at));
  return fresh[0];
}

// waitForSession polls until the terminal's session shows up: the agent
// writes its record once the relay has registered it.
export async function waitForSession(
  bin: string,
  pid: () => Promise<number | undefined>,
  cwd: string | undefined,
  since: number,
  timeoutMs = 30000,
  isCancelled: () => boolean = () => false,
): Promise<SessionInfo | undefined> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline && !isCancelled()) {
    const s = matchSession(await listSessions(bin), await pid(), cwd, since);
    if (s) return s;
    await new Promise((r) => setTimeout(r, 500));
  }
  return undefined;
}
