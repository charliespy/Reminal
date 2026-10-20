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
  // "port" for a `reminal expose` forward, with the local port it shares.
  // Absent for a shell, and from a reminal that doesn't list forwards.
  kind?: string;
  port?: number;
}

export function isPort(s: SessionInfo): boolean {
  return s.kind === "port";
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

// queryAll is every live shell session on this machine and every port it
// shares (`reminal expose`), or undefined when reminal couldn't be asked (it
// failed, or timed out). A reminal too old to list forwards ignores --ports
// and gives the shells alone.
export function queryAll(bin: string): Promise<SessionInfo[] | undefined> {
  return new Promise((resolve) => {
    execFile(bin, ["info", "--all", "--json", "--ports"], { env: processEnv(), timeout: 5000, windowsHide: true }, (err, stdout) => {
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

// querySessions is queryAll's shell sessions.
export async function querySessions(bin: string): Promise<SessionInfo[] | undefined> {
  return (await queryAll(bin))?.filter((s) => !isPort(s));
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

// ---- reminal copy / paste / expose ----

export interface RunResult {
  ok: boolean;
  stdout: string;
  stderr: string;
  // timedOut: it ran past its time and was stopped.
  timedOut?: boolean;
  // cancelled: the user stopped it.
  cancelled?: boolean;
}

// runReminal runs one reminal command and gives back what it printed.
export function runReminal(bin: string, args: string[], timeoutMs: number, cwd?: string, signal?: AbortSignal): Promise<RunResult> {
  return new Promise((resolve) => {
    execFile(bin, args, { env: processEnv(), timeout: timeoutMs, windowsHide: true, cwd, signal }, (err, stdout, stderr) => {
      const cancelled = !!signal?.aborted;
      resolve({
        ok: !err,
        stdout: String(stdout ?? ""),
        stderr: String(stderr ?? "") || (err && !stdout ? String(err.message) : ""),
        timedOut: !cancelled && !!err && (err as { killed?: boolean }).killed === true,
        cancelled,
      });
    });
  });
}

// copyCodeOf is the code `reminal copy` printed ("Copy code: ABCDE-FGHJK").
export function copyCodeOf(stdout: string): string | undefined {
  // Only a code of the shape reminal hands out (an older reminal's is 8
  // characters): anything else is never shown or copied as one.
  return /^Copy code:\s*([A-Z0-9]{5}-[A-Z0-9]{5}|[A-Z0-9]{4}-[A-Z0-9]{4})\s*$/m.exec(stdout)?.[1];
}

// pasteCode takes what someone typed or pasted into the Paste box: the code
// alone, in any case and with or without its dash, or the whole
// `reminal paste <code>` line. It gives the code as reminal prints it, or
// undefined when there is none.
export function pasteCode(input: string): string | undefined {
  // The code may be typed in two halves; a path after it is not part of it.
  const words = input.trim().replace(/^reminal\s+paste\s+/i, "").split(/\s+/);
  const alnum = (w: string | undefined, n: number) => !!w && new RegExp(`^[A-Za-z0-9]{${n}}$`).test(w);
  let code = words[0]?.replace(/^([A-Za-z0-9]{4,5})-([A-Za-z0-9]{4,5})$/, "$1$2") ?? "";
  if (alnum(words[0], 5) && alnum(words[1], 5)) code = words[0] + words[1];
  if (!alnum(code, 10) && !alnum(code, 8)) return undefined;
  code = code.toUpperCase();
  return code.length === 10 ? `${code.slice(0, 5)}-${code.slice(5)}` : code; // 8: an older reminal's code; reminal explains
}

// savedPathOf is where `reminal paste` put what it received ("Saved <path>").
export function savedPathOf(stdout: string): string | undefined {
  return /^Saved (.+)$/m.exec(stdout)?.[1]?.trim();
}

// exposedOf is the public link and PIN `reminal expose` printed.
export function exposedOf(stdout: string): { url?: string; pin?: string; link?: string } {
  return {
    url: /Public URL:\s*(\S+)/.exec(stdout)?.[1],
    pin: /PIN:\s*(\d+)/.exec(stdout)?.[1],
    link: /Quick link:\s*(\S+)/.exec(stdout)?.[1],
  };
}

// problemOf is the line of reminal's output worth showing someone when a
// command fails: its last non-empty line, without a leading "error: ", as
// plain text (it can carry names chosen on another machine).
export function problemOf(r: RunResult): string {
  if (r.timedOut) return "it took too long and was stopped.";
  const lines = (r.stderr || r.stdout).split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
  return plainText((lines[lines.length - 1] ?? "reminal didn't say why.").replace(/^error:\s*/i, ""));
}

// plainText is text from outside the extension (reminal's output, names
// from another machine) made fit for a notification, which reads
// [text](target) as a link: no brackets, no control or direction-changing
// characters, and not too long.
export function plainText(s: string, max = 300): string {
  const t = s.replace(/[[\]]/g, "").replace(/[\u0000-\u001f\u007f-\u009f\u200e\u200f\u202a-\u202e\u2066-\u2069]/g, "");
  return t.length > max ? `${t.slice(0, max)}…` : t;
}

// stopArgs stops a shared port by its forward's id, so it is that forward
// and no other that stops; undefined for an id that isn't one.
export function stopArgs(s: SessionInfo): string[] | undefined {
  if (!/^[A-Za-z0-9]+$/.test(s.id)) return undefined;
  return ["stop", "--yes", "--", s.id];
}

// stillShared is the forward with this id among what reminal lists now, if
// it is still a shared port.
export function stillShared(list: SessionInfo[] | undefined, id: string): SessionInfo | undefined {
  return list?.find((x) => x.id === id && isPort(x));
}

// alreadyExposedOf is the link `reminal expose` printed for a port that was
// already shared ("Port 3000 is already exposed: <url>").
export function alreadyExposedOf(stdout: string): string | undefined {
  return /already exposed:\s*(\S+)/.exec(stdout)?.[1];
}

// isWebLink is a link that is safe to hand to the browser: http or https.
export function isWebLink(u: string | undefined): u is string {
  return !!u && /^https?:\/\//i.test(u);
}

// validPort is the port in what someone typed ("3000" or "localhost:3000"),
// or undefined when it isn't one.
export function validPort(v: string): number | undefined {
  const t = v.trim().replace(/^(localhost|127\.0\.0\.1):/i, "");
  if (!/^\d{1,5}$/.test(t)) return undefined;
  const n = Number(t);
  return n >= 1 && n <= 65535 ? n : undefined;
}

// ---- sessions started elsewhere ----

// attachArgs opens a session that is already running, by its exact id: the
// arguments for a terminal that views it. undefined for an id that isn't one.
export function attachArgs(id: string): string[] | undefined {
  return /^[A-Za-z0-9]+$/.test(id) ? ["attach", id] : undefined;
}

// attachedId is the session a terminal opened with attachArgs views, read
// back from its arguments (a window reload brings such a terminal back with
// them), or undefined for any other terminal.
export function attachedId(shellArgs: string[] | string | undefined): string | undefined {
  if (!Array.isArray(shellArgs) || shellArgs.length !== 2 || shellArgs[0] !== "attach") return undefined;
  return attachArgs(shellArgs[1]) ? shellArgs[1] : undefined;
}

// otherSessions are the shell sessions on this machine that no terminal in
// this window shows, newest first, leaving out the one the editor itself was
// started from.
export function otherSessions(all: SessionInfo[], shown: Set<string>, self?: string): SessionInfo[] {
  return all
    .filter((s) => !isPort(s) && !shown.has(s.id) && s.id !== self)
    .sort((a, b) => Date.parse(b.started_at) - Date.parse(a.started_at));
}

// tabName is a session's name as a terminal tab shows it: one line, no
// control or direction-changing characters, not too long. undefined for a
// session with no name.
export function tabName(name: string | undefined): string | undefined {
  const t = (name ?? "").replace(/[\u0000-\u001f\u007f-\u009f‎‏‪-‮⁦-⁩]/g, " ").replace(/\s+/g, " ").trim();
  if (!t) return undefined;
  return t.length > 60 ? `${t.slice(0, 60)}…` : t;
}
