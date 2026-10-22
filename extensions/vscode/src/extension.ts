// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

import * as path from "node:path";
import * as vscode from "vscode";
import {
  CLEAN_ENV,
  INSTALL_UNIX,
  INSTALL_URL,
  INSTALL_WINDOWS,
  STATUS_LABEL,
  SessionInfo,
  Status,
  attachArgs,
  attachedId,
  findReminal,
  isPort,
  isWebLink,
  listSessions,
  otherSessions,
  plainText,
  queryAll,
  querySessions,
  renamedTo,
  runReminal,
  statusOf,
  summary,
  tabName,
  waitForSession,
} from "./reminal";
import { shareAgentText, shareHumanText } from "./share";
import { TabOrder } from "./tabs";
import { PortItem, SHARE_PORT_ITEM, copyItem, pasteHere, portActions, portItems, sharePort } from "./transfer";

const PROFILE_ID = "reminal.terminal";
const TERMINAL_NAME = "reminal";
const POLL_MS = 3000;
const PID_WAIT_MS = 2000;

// The session each reminal terminal turned out to be, once known. A Map, not
// a WeakMap, because the status bar walks it; closed terminals are removed.
const sessions = new Map<vscode.Terminal, SessionInfo>();
// Terminals this extension started: they need no second "close the old one?"
// and their session is looked up as soon as they open.
const ours = new WeakSet<vscode.Terminal>();
// Terminals whose session has left reminal's list: sharing stopped (Ctrl-])
// or reminal exited. Their link and PIN would let no one in.
const stopped = new WeakSet<vscode.Terminal>();

function config() {
  return vscode.workspace.getConfiguration("reminal");
}

// configuredPath is reminal.path from the user's own settings only, never a
// folder's .vscode/settings.json: where reminal is installed is a fact about
// this machine, not about a project. The setting's machine scope already
// keeps folders out; reading only the user's value makes sure.
function configuredPath(): string {
  return config().inspect<string>("path")?.globalValue?.trim() ?? "";
}

function resolveBinary(): string | undefined {
  return findReminal(configuredPath());
}

async function notInstalled(): Promise<void> {
  const configured = configuredPath();
  const msg = configured
    ? `reminal was not found at ${configured}. Fix the "reminal.path" setting, or clear it to search your PATH.`
    : "reminal is not installed. Install it with one command from reminal.app, then try again.";
  const choice = await vscode.window.showErrorMessage(
    msg,
    ...(configured ? [] : ["Install reminal"]),
    "Copy install command",
    "Open reminal.app",
    ...(configured ? ["Open settings"] : []),
  );
  if (choice === "Install reminal") {
    installInTerminal();
  } else if (choice === "Copy install command") {
    await vscode.env.clipboard.writeText(process.platform === "win32" ? INSTALL_WINDOWS : INSTALL_UNIX);
    vscode.window.showInformationMessage("Install command copied. Paste it into a terminal.");
  } else if (choice === "Open reminal.app") {
    vscode.env.openExternal(vscode.Uri.parse(INSTALL_URL));
  } else if (choice === "Open settings") {
    vscode.commands.executeCommand("workbench.action.openSettings", "reminal.path");
  }
}

// installInTerminal runs the reminal.app install command in a terminal the
// user can see, and can answer the installer's questions in, then offers a
// reminal terminal once reminal can be found.
const INSTALL_WAIT_MS = 15 * 60 * 1000;
let install: { t: vscode.Terminal; stop: () => void } | undefined;

function installInTerminal(): void {
  if (install && vscode.window.terminals.includes(install.t)) {
    install.t.show();
    return;
  }
  install?.stop();
  const cwd = cwdOf(vscode.window.activeTerminal);
  const win = process.platform === "win32";
  // The Windows command is PowerShell, whatever the default shell is.
  const ps = process.env.SystemRoot
    ? path.join(process.env.SystemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
    : "powershell.exe";
  const t = vscode.window.createTerminal({ name: "Install reminal", ...(win ? { shellPath: ps } : {}) });
  t.show();
  t.sendText(win ? INSTALL_WINDOWS : INSTALL_UNIX);
  // reminal is in place before the installer's last questions, so where the
  // terminal reports when a command ends, that is waited for too. The exit
  // code says whether the install failed.
  let ended: number | undefined | null = null; // null: still running
  const subs = [
    vscode.window.onDidEndTerminalShellExecution((e) => {
      if (e.terminal === t) ended = e.exitCode;
    }),
    vscode.window.onDidCloseTerminal((c) => {
      if (c === t && !resolveBinary()) stop(); // given up on
    }),
  ];
  const since = Date.now();
  const timer = setInterval(() => {
    const found = resolveBinary();
    if (!found && ended !== null && ended !== 0) {
      stop();
      void vscode.window
        .showErrorMessage("The reminal install didn't finish. The \"Install reminal\" terminal shows what went wrong.", "Show terminal")
        .then((c) => c && t.show());
      return;
    }
    // Without shell integration the end of the command can't be seen; once
    // reminal is there, say so and let the user judge the installer.
    // reminal can be in place while the installer's setup questions after it
    // ended in an error or Ctrl-C: reminal works, and the terminal says what
    // was skipped.
    const tracked = !!t.shellIntegration;
    if (found && (ended !== null || !tracked || t.exitStatus !== undefined)) {
      stop();
      const failed = typeof ended === "number" && ended !== 0;
      void vscode.window
        .showInformationMessage(
          failed
            ? "reminal is installed, but its setup didn't finish. The \"Install reminal\" terminal shows what was skipped."
            : tracked
              ? "reminal is installed."
              : "reminal is installed. Once the installer has finished, open a reminal terminal.",
          "New reminal Terminal Here",
          ...(failed ? ["Show terminal"] : []),
        )
        .then((c) => {
          if (c === "Show terminal") t.show();
          else if (c) openReminal(cwd);
        });
      return;
    }
    if (Date.now() - since > INSTALL_WAIT_MS) stop();
  }, 2000);
  function stop(): void {
    clearInterval(timer);
    for (const d of subs) d.dispose();
    if (install?.t === t) install = undefined;
  }
  install = { t, stop };
}

function terminalOptions(bin: string, cwd?: string): vscode.TerminalOptions {
  return {
    name: TERMINAL_NAME,
    shellPath: bin,
    cwd,
    env: CLEAN_ENV,
    iconPath: new vscode.ThemeIcon("cloud"),
  };
}

// cwdOf is the folder a terminal is in now if shell integration knows it,
// else the folder it was started in, else the workspace's first folder. It is
// a plain path, never a Uri: in a remote window (SSH, a container, a browser
// editor) a file: Uri as a terminal's cwd asks for a terminal on the local
// side instead of the remote one, where reminal and the folder are.
function cwdOf(t: vscode.Terminal | undefined): string | undefined {
  const live = t?.shellIntegration?.cwd;
  if (live) return live.fsPath;
  const opts = t?.creationOptions as vscode.TerminalOptions | undefined;
  if (opts?.cwd) return typeof opts.cwd === "string" ? opts.cwd : opts.cwd.fsPath;
  return vscode.workspace.workspaceFolders?.[0]?.uri.fsPath;
}

function isReminalTerminal(t: vscode.Terminal, bin: string | undefined): boolean {
  const opts = t.creationOptions as vscode.TerminalOptions;
  if (opts?.hideFromUser || t.name === PLACEHOLDER_NAME) return false; // the profile's placeholder
  return ours.has(t) || (!!bin && opts?.shellPath === bin);
}

async function showLink(s: SessionInfo, intro?: string): Promise<void> {
  const head = intro ?? `reminal session ${plain(s.name || s.id)}`;
  const choice = await vscode.window.showInformationMessage(
    `${head}. PIN ${s.pin}. Open it from any browser or phone.`,
    "Copy link",
    "Open in browser",
  );
  if (choice === "Copy link") {
    await vscode.env.clipboard.writeText(s.join_url);
    vscode.window.setStatusBarMessage("reminal link copied", 3000);
  } else if (choice === "Open in browser") {
    if (isWebLink(s.join_url)) vscode.env.openExternal(vscode.Uri.parse(s.join_url));
  }
}

// ---- status: Working / Needs you / Done ----
//
// VS Code gives an extension no way to change a terminal tab's icon or color
// after the terminal exists, and its name only while it is the active one
// (Terminal.name is read-only; workbench.action.terminal.renameWithArg
// renames the active terminal). What it does allow is a file decoration: the tab list draws every terminal
// from a vscode-terminal:/<workspace>/<id> URI and asks decoration providers
// about it (terminalTabsList.ts), so a badge, a colour and a tooltip can sit
// beside the name. Which terminal a URI is, TabOrder works out (tabs.ts). The
// status bar can sum it all up as well (reminal.statusBar).

let bar: vscode.StatusBarItem;

const order = new TabOrder<vscode.Uri>();
// window.terminals as of the last open: a closed terminal is already gone
// from window.terminals when onDidCloseTerminal fires, and its place in this
// copy is what says which id was its.
let known: vscode.Terminal[] = [];
const decorationsChanged = new vscode.EventEmitter<vscode.Uri[] | undefined>();
const DECORATION: Record<Status, { badge: string; color: string }> = {
  needs: { badge: "!", color: "charts.orange" },
  working: { badge: "\u2026", color: "charts.blue" }, // …
  done: { badge: "\u2713", color: "charts.green" }, // ✓
};

function instanceId(uri: vscode.Uri): number | undefined {
  const id = Number.parseInt(uri.path.split("/")[2] ?? "", 10);
  return Number.isFinite(id) && id > 0 ? id : undefined;
}

// tabbed is window.terminals without the ones an extension hid from the user:
// those have an instance id but no tab, so no URI ever comes for them.
function tabbed(): readonly vscode.Terminal[] {
  return vscode.window.terminals.filter((t) => !(t.creationOptions as vscode.TerminalOptions).hideFromUser);
}

// terminalFor is the terminal a tab's URI draws, or undefined when it can't
// be told for sure.
function terminalFor(uri: vscode.Uri): vscode.Terminal | undefined {
  const id = instanceId(uri);
  if (id === undefined) return undefined;
  const terms = tabbed();
  const { index, again } = order.ask(id, uri, terms.length);
  if (again) setTimeout(() => decorationsChanged.fire(undefined), 0);
  return index === undefined ? undefined : terms[index];
}

function uriFor(t: vscode.Terminal): vscode.Uri | undefined {
  const terms = tabbed();
  return order.uriAt(terms.indexOf(t), terms.length);
}

const decorations: vscode.FileDecorationProvider = {
  onDidChangeFileDecorations: decorationsChanged.event,
  provideFileDecoration(uri) {
    if (uri.scheme !== "vscode-terminal") return undefined;
    const t = terminalFor(uri);
    const st = t && statusFor(t);
    if (!st) return undefined;
    const d = DECORATION[st];
    return new vscode.FileDecoration(d.badge, `reminal: ${STATUS_LABEL[st]}`, new vscode.ThemeColor(d.color));
  },
};
const shownStatus = new Map<vscode.Terminal, Status | undefined>();
let timer: NodeJS.Timeout | undefined;
let polling = false;

// label is how a terminal is told apart in lists: its session's name, else
// its folder's name.
function label(t: vscode.Terminal, s: SessionInfo): string {
  if (tabName(s.name)) return plain(tabName(s.name)!);
  const dir = s.cwd ?? cwdOf(t);
  return plain(dir ? path.basename(dir) || dir : t.name);
}

// plain is a folder or terminal name as it goes into a notification, which
// reads [text](target) as a link: without the brackets it stays just text.
function plain(s: string): string {
  return plainText(s, 500);
}

// The name each terminal's tab was last given from its session, so a tab
// renamed by hand keeps that name until the session's own name changes.
const named = new WeakMap<vscode.Terminal, string>();

// syncName gives a terminal's tab its session's name. The editor renames only
// the active terminal, so a terminal that isn't gets it when it next is.
function syncName(t: vscode.Terminal): void {
  const name = tabName(sessions.get(t)?.name);
  if (!name || named.get(t) === name) return;
  if (t.name === name) {
    named.set(t, name);
    return;
  }
  if (vscode.window.activeTerminal !== t) return;
  named.set(t, name);
  void vscode.commands.executeCommand("workbench.action.terminal.renameWithArg", { name });
}

// The name each reminal terminal's tab had at the last look, so that a
// rename by hand can be told from no change.
const seen = new WeakMap<vscode.Terminal, string>();

// passRename gives a terminal's session the name its tab was given by hand,
// so the session goes by it everywhere: this window's lists, a browser,
// `reminal list`.
function passRename(t: vscode.Terminal, bin: string): void {
  const s = sessions.get(t);
  if (!s || stopped.has(t)) return;
  const exe = path.basename(bin);
  const name = renamedTo({
    tab: t.name,
    seen: seen.get(t),
    given: named.get(t),
    session: s.name,
    defaults: [TERMINAL_NAME, PLACEHOLDER_NAME, exe, exe.replace(/\.exe$/i, "")],
  });
  seen.set(t, t.name);
  if (!name) return;
  named.set(t, tabName(name)!);
  sessions.set(t, { ...s, name });
  void runReminal(bin, ["rename", s.id, name], 10000);
}

function statusFor(t: vscode.Terminal): Status | undefined {
  return statusOf(sessions.get(t)?.attn);
}

function render(): void {
  updateContext();
  const changed: vscode.Uri[] = [];
  let unknown = false;
  for (const t of sessions.keys()) {
    const st = statusFor(t);
    if (shownStatus.get(t) === st && shownStatus.has(t)) continue;
    shownStatus.set(t, st);
    const uri = uriFor(t);
    if (uri) changed.push(uri);
    else unknown = true; // not drawn yet, or drawn before it was a session
  }
  if (unknown) decorationsChanged.fire(undefined);
  else if (changed.length) decorationsChanged.fire(changed);
  if (sessions.size === 0 || !config().get<boolean>("statusBar", false)) {
    bar.hide();
    return;
  }
  const terms = [...sessions.keys()];
  const { text, top } = summary(terms.map(statusFor));
  bar.text = text;
  bar.backgroundColor = top === "needs" ? new vscode.ThemeColor("statusBarItem.warningBackground") : undefined;
  const md = new vscode.MarkdownString("**reminal terminals**\n\n");
  for (const t of terms) {
    const st = statusFor(t);
    md.appendMarkdown("- ");
    md.appendText(`${label(t, sessions.get(t)!)}: ${st ? STATUS_LABEL[st] : "Shell"}`);
    md.appendMarkdown("\n");
  }
  md.appendMarkdown("\nClick to go to one.");
  bar.tooltip = md;
  bar.show();
}

async function poll(bin: string): Promise<void> {
  if (polling) return; // a slow `reminal info` never stacks up
  polling = true;
  try {
    // A failed or slow `reminal info` says nothing about the sessions: keep
    // what is shown rather than blank every badge and re-announce "needs you".
    const list = await querySessions(bin);
    if (!list) return;
    await adoptFrom(list, (t) => isReminalTerminal(t, bin));
    const live = new Map(list.map((s) => [s.id, s]));
    for (const [t, prev] of sessions) {
      const now = live.get(prev.id);
      if (!now) {
        // Sharing stopped (Ctrl-]) or the session ended: no state to show.
        sessions.set(t, { ...prev, attn: undefined });
        stopped.add(t);
        continue;
      }
      sessions.set(t, now);
      stopped.delete(t);
      passRename(t, bin);
      syncName(t);
      if (statusOf(now.attn) === "needs" && statusOf(prev.attn) !== "needs") void needsYou(t, now);
    }
    render();
  } finally {
    polling = false;
  }
}

// needsYou offers a nudge when a terminal starts waiting on you, unless you
// are already looking at it.
async function needsYou(t: vscode.Terminal, s: SessionInfo): Promise<void> {
  if (!config().get<boolean>("notifyNeedsYou", true)) return;
  if (vscode.window.state.focused && vscode.window.activeTerminal === t) return;
  const choice = await vscode.window.showInformationMessage(
    `reminal in ${label(t, s)} needs you.`,
    "Show terminal",
  );
  if (choice === "Show terminal") t.show();
}

// pending is the reminal terminals whose session isn't known yet: one still
// starting, or one restored before this extension host started.
function pending(bin: string | undefined): vscode.Terminal[] {
  return vscode.window.terminals.filter(
    (t) => !sessions.has(t) && !tracking.has(t) && t.exitStatus === undefined && isReminalTerminal(t, bin),
  );
}

// The poll runs only while there are reminal terminals to report on.
function schedule(): void {
  const bin = resolveBinary();
  const needed = sessions.size > 0 || pending(bin).length > 0;
  if (needed && bin && !timer) {
    // The path is looked up each time: reminal.path may have changed.
    timer = setInterval(() => {
      const b = resolveBinary();
      if (b) void poll(b);
    }, POLL_MS);
  } else if ((!needed || !bin) && timer) {
    clearInterval(timer);
    timer = undefined;
  }
  render();
}

async function goToTerminal(): Promise<void> {
  const terms = [...sessions.keys()];
  const waiting = terms.filter((t) => statusFor(t) === "needs");
  if (waiting.length === 1) return waiting[0].show();
  const order: Record<string, number> = { needs: 0, working: 1, done: 2 };
  const items = terms
    .map((t) => {
      const st = statusFor(t);
      return { t, st, label: label(t, sessions.get(t)!), description: st ? STATUS_LABEL[st] : "" };
    })
    .sort((a, b) => (order[a.st ?? ""] ?? 3) - (order[b.st ?? ""] ?? 3));
  const pick = await vscode.window.showQuickPick(items, { placeHolder: "Go to a reminal terminal" });
  pick?.t.show();
}

// track finds the session a reminal terminal is running and, if the setting
// is on, offers its link.
const tracking = new WeakSet<vscode.Terminal>();

async function track(t: vscode.Terminal, bin: string, announce: boolean): Promise<void> {
  const since = Date.now();
  let closed = false;
  tracking.add(t);
  const sub = vscode.window.onDidCloseTerminal((c) => {
    if (c === t) closed = true;
  });
  try {
    const s = await waitForSession(bin, () => Promise.resolve(t.processId), cwdOf(t), since, 30000, () => closed);
    tracking.delete(t);
    if (closed) return;
    if (!s) {
      // Not registered in time (offline, a slow relay): the poll keeps
      // looking for it by its pid.
      schedule();
      return;
    }
    sessions.set(t, s);
    syncName(t);
    schedule();
    if (announce) await announceSession(t, s);
  } finally {
    tracking.delete(t);
    sub.dispose();
  }
}

async function announceSession(t: vscode.Terminal, s: SessionInfo): Promise<void> {
  if (!config().get<boolean>("showLinkOnStart", true)) return;
  const where = s.cwd ?? cwdOf(t);
  await showLink(s, where ? `reminal is sharing ${plain(where)}` : undefined);
}

// adopt picks up reminal terminals this extension host did not open: the ones
// a window reload or a reconnect brings back, which are still sessions but
// which a fresh extension host knows nothing about. A terminal whose process
// is a live session's pid is that session. Restored terminals can take a few
// seconds to report their pid, so it looks a few times; after that the poll
// keeps looking for any reminal terminal still without a session.
let adopting = false;
async function adopt(bin: string): Promise<void> {
  if (adopting) return;
  adopting = true;
  try {
    for (let i = 0; i < 5; i++) {
      if (vscode.window.terminals.every((t) => sessions.has(t))) break;
      const list = await listSessions(bin);
      // A session no terminal here has is what a restored terminal could be;
      // with none, a plain shell that just opened needs no more looking.
      const had = new Set([...sessions.values()].map((s) => s.id));
      if (!list.some((s) => !had.has(s.id))) break;
      await adoptFrom(list, () => true);
      schedule();
      await new Promise((r) => setTimeout(r, 2000));
    }
  } finally {
    adopting = false;
  }
}


// When this extension host started. A session that began around then or later
// in a terminal it did not open is not one carried over from before a window
// reload: the editor restarted and ran the terminal's command again, which
// started a new share with a new link. That is said, as for any new share.
const hostStart = Date.now() - process.uptime() * 1000;

async function adoptFrom(list: SessionInfo[], which: (t: vscode.Terminal) => boolean): Promise<void> {
  forgetEndedViewers(list);
  for (const t of vscode.window.terminals) {
    if (sessions.has(t) || tracking.has(t) || !which(t)) continue;
    // A terminal whose process never started never reports a pid; waiting on
    // it would hold up every poll after this one.
    const pid = await Promise.race([t.processId, new Promise<undefined>((r) => setTimeout(() => r(undefined), PID_WAIT_MS))]);
    // One that views a session started elsewhere is that session, by id: its
    // process is the viewer, not the session.
    const viewing = attachedId((t.creationOptions as vscode.TerminalOptions).shellArgs) ?? (pid ? viewers()[pid] : undefined);
    if (viewing) {
      const s = list.find((x) => x.id === viewing);
      if (s && vscode.window.terminals.includes(t) && !sessions.has(t)) {
        if (pid) viewerPid.set(t, pid);
        ours.add(t);
        sessions.set(t, s);
        syncName(t);
      }
      continue;
    }
    const s = pid ? list.find((x) => x.pid === pid) : undefined;
    // Closed while its pid was awaited: its close has been handled already.
    if (!s || !vscode.window.terminals.includes(t) || sessions.has(t)) continue;
    ours.add(t);
    sessions.set(t, s);
    syncName(t);
    if (Date.parse(s.started_at) >= hostStart - 60000) void announceSession(t, s);
  }
}

function openReminal(cwd: string | undefined): vscode.Terminal | undefined {
  const bin = resolveBinary();
  if (!bin) {
    void notInstalled();
    return undefined;
  }
  const t = vscode.window.createTerminal(terminalOptions(bin, cwd));
  ours.add(t);
  t.show();
  void track(t, bin, true);
  return t;
}

// viewers is which session each viewing terminal's process views, by pid,
// kept with the workspace: a window reload reconnects to the same process
// but gives the extension none of the arguments it was started with.
let workspaceState: vscode.Memento | undefined;
const VIEWERS_KEY = "reminal.viewers";
function viewers(): Record<string, string> {
  return workspaceState?.get<Record<string, string>>(VIEWERS_KEY) ?? {};
}
function setViewer(pid: number, id: string | undefined): void {
  const v = { ...viewers() };
  if (id) v[pid] = id;
  else delete v[pid];
  void workspaceState?.update(VIEWERS_KEY, v);
}
const viewerPid = new WeakMap<vscode.Terminal, number>();
// A viewing terminal that closes is forgotten a little later, not at once:
// a window reload closes every terminal in the extension host it ends, and
// that host is gone before then, so what it views is still known after.
const FORGET_VIEWER_MS = 10000;

// forgetEndedViewers drops the viewing terminals whose session has ended, so
// a pid used again later, by some other terminal, is not taken for one.
function forgetEndedViewers(list: SessionInfo[]): void {
  const v = viewers();
  const ids = new Set(list.map((s) => s.id));
  const kept = Object.fromEntries(Object.entries(v).filter(([, id]) => ids.has(id)));
  if (Object.keys(kept).length !== Object.keys(v).length) void workspaceState?.update(VIEWERS_KEY, kept);
}

// openExisting opens a session that is already running (started from
// another terminal, a phone, or `reminal new`) in a terminal that views it.
// Closing that terminal leaves the session running.
function openExisting(bin: string, s: SessionInfo): void {
  const args = attachArgs(s.id);
  if (!args) return;
  const t = vscode.window.createTerminal({
    name: tabName(s.name) ?? TERMINAL_NAME,
    shellPath: bin,
    shellArgs: args,
    env: CLEAN_ENV,
    iconPath: new vscode.ThemeIcon("cloud"),
  });
  ours.add(t);
  sessions.set(t, s);
  if (tabName(s.name)) named.set(t, tabName(s.name)!);
  t.show();
  schedule();
  void t.processId.then((pid) => {
    if (!pid || !vscode.window.terminals.includes(t)) return;
    viewerPid.set(t, pid);
    setViewer(pid, s.id);
  });
}

// The argument a terminal menu passes is the terminal it was opened on; from
// the command palette there is none, and the active terminal is meant.
function terminalArg(arg: unknown): vscode.Terminal | undefined {
  const t = vscode.window.terminals.find((x) => x === arg);
  return t ?? vscode.window.activeTerminal;
}

// openInFolder opens a new reminal terminal in another terminal's folder. Only
// the folder carries over: the old shell, its history and anything running in
// it stay where they are, so closing it is offered, never assumed.
async function openInFolder(arg: unknown): Promise<void> {
  const from = terminalArg(arg);
  const bin = resolveBinary();
  if (from && isReminalTerminal(from, bin) && !stopped.has(from)) {
    const s = sessions.get(from);
    if (s) await showLink(s);
    else vscode.window.showInformationMessage("This terminal is already a reminal session.");
    return;
  }
  const t = openReminal(cwdOf(from));
  if (!t || !from) return;
  const mode = config().get<string>("closeOriginal", "ask");
  if (mode === "always") {
    from.dispose();
  } else if (mode === "ask") {
    const choice = await vscode.window.showInformationMessage(
      `Opened a new reminal terminal in the same folder. Close "${plain(from.name)}" too? Anything still running in it stops.`,
      "Close it",
      "Keep it",
    );
    if (choice === "Close it") from.dispose();
  }
}

// notSharing says a reminal terminal's session has stopped sharing, and offers
// a new one in its folder.
async function notSharing(t: vscode.Terminal, s: SessionInfo): Promise<void> {
  const choice = await vscode.window.showInformationMessage(
    `${label(t, s)} isn't sharing any more, so there is nothing to copy.`,
    "Open reminal in This Folder",
  );
  if (choice) await openInFolder(t);
}

// ---- share ----
//
// The web viewer's two share buttons, for a reminal terminal: one text for a
// person, one for an AI agent (share.ts). VS Code gives an extension no way to
// add a button to a terminal's row in the tab list, and no context key that
// describes the tab that was right-clicked (right-clicking only moves the
// list's focus; every per-terminal key follows the active terminal). So the
// items show on every tab while any reminal terminal is open, and act on the
// terminal the menu was opened on, which the command is given.

type Audience = "person" | "agent";

// updateContext sets the key the menus read, when it changes.
let hasTerminals: boolean | undefined;
function updateContext(): void {
  if (hasTerminals === sessions.size > 0) return;
  hasTerminals = sessions.size > 0;
  void vscode.commands.executeCommand("setContext", "reminal.hasTerminals", hasTerminals);
}

async function share(arg: unknown, audience: Audience): Promise<void> {
  const t = terminalArg(arg);
  const rec = t && sessions.get(t);
  if (!t || !rec) {
    if (t && isReminalTerminal(t, resolveBinary())) {
      vscode.window.showInformationMessage("This reminal terminal is still starting. Try again in a moment.");
      return;
    }
    const choice = await vscode.window.showInformationMessage(
      "This terminal isn't a reminal session, so there is nothing to share yet.",
      "Open reminal in This Folder",
    );
    if (choice) await openInFolder(t);
    return;
  }
  // The latest record: a `reminal repin` since it started changes the PIN. A
  // session missing from a list that did come back has stopped sharing, and
  // its old link and PIN would let no one in.
  const bin = resolveBinary();
  const list = bin ? await querySessions(bin) : undefined;
  const latest = list?.find((x) => x.id === rec.id);
  if (list && !latest) {
    stopped.add(t);
    return notSharing(t, rec);
  }
  const s = latest ?? rec;
  const text = audience === "person" ? shareHumanText(s.id, s.pin, s.join_url) : shareAgentText(s.id, s.pin, s.join_url);
  await vscode.env.clipboard.writeText(text);
  vscode.window.showInformationMessage(
    audience === "person"
      ? `Copied a join message for ${label(t, s)}. Paste it to the person you're sharing with.`
      : `Copied instructions for an AI agent to join ${label(t, s)}. Paste them into the agent's chat.`,
  );
}

// The title-bar cloud button lists this window's reminal terminals, in the
// order they were opened, with each one's state, then the other reminals
// running on this machine, which open here, then the ports reminal is sharing
// on this machine, and ends with rows that open a new terminal in the active
// terminal's folder and share another port. Sharing a terminal stays in its
// tab's right-click menu.
type ListItem = PortItem & { t?: vscode.Terminal; open?: boolean; existing?: SessionInfo };

async function cloudButton(): Promise<void> {
  const active = vscode.window.activeTerminal;
  const bin = resolveBinary();
  const items: ListItem[] = vscode.window.terminals
    .filter((t) => sessions.has(t) || isReminalTerminal(t, bin))
    .map((t) => {
      const s = sessions.get(t);
      if (!s) return { t, label: `$(cloud) ${t.name}`, description: t.exitStatus ? "Ended" : "Starting" };
      const st = statusFor(t);
      const state = stopped.has(t) ? "Not sharing" : st ? STATUS_LABEL[st] : "Shell";
      return { t, label: `$(cloud) ${label(t, s)}`, description: `${state} · ${s.id.toUpperCase()}` };
    });
  const terminals = items.length;
  // Ports are machine-wide and may be shared from anywhere, so they are
  // asked for now rather than taken from the poll, which runs only while a
  // reminal terminal is open.
  const all = bin ? await queryAll(bin) : undefined;
  const shown = new Set([...sessions.entries()].filter(([t]) => !stopped.has(t)).map(([, s]) => s.id));
  const others = otherSessions(all ?? [], shown, process.env.REMINAL_SESSION);
  if (others.length) items.push({ label: "Other reminals on this machine", kind: vscode.QuickPickItemKind.Separator });
  for (const s of others) {
    const st = statusOf(s.attn);
    const where = s.cwd ? path.basename(s.cwd) || s.cwd : undefined;
    items.push({
      existing: s,
      label: `$(terminal) ${plain(tabName(s.name) ?? where ?? s.id.toUpperCase())}`,
      description: [st ? STATUS_LABEL[st] : "Shell", s.id.toUpperCase()].join(" · "),
      detail: s.cwd ? plain(s.cwd) : undefined,
    });
  }
  items.push(...portItems((all ?? []).filter(isPort)));
  if (items.length) items.push({ label: "", kind: vscode.QuickPickItemKind.Separator });
  items.push({ label: "$(add) New reminal Terminal Here", open: true }, SHARE_PORT_ITEM);
  const pick = await vscode.window.showQuickPick(items, {
    placeHolder: terminals
      ? "reminal terminals in this window"
      : others.length
        ? "Open a reminal running on this machine, or start a new one"
        : "No reminal terminals open yet",
  });
  if (pick?.open) openReminal(cwdOf(active));
  else if (pick?.existing) {
    if (bin) openExisting(bin, pick.existing);
  }
  else if (pick?.sharePort) {
    if (bin) await sharePort(deps);
    else void notInstalled();
  } else if (pick?.port) await portActions(deps, pick.port);
  else pick?.t?.show();
}

// deps is what transfer.ts needs from here: the reminal program, or the
// not-installed message when there is none.
const deps = {
  binary: (): string | undefined => {
    const bin = resolveBinary();
    if (!bin) void notInstalled();
    return bin;
  },
};

// The editor makes the last terminal in its list the active one as soon as a
// profile provider returns, before the terminal the profile describes exists,
// so for a moment another terminal shows. The reminal profile therefore opens
// the reminal terminal itself and waits until the editor has it, then returns
// a placeholder that is hidden and ends at once.
const PLACEHOLDER_NAME = "reminal (opening)";

// It runs `reminal version`, which prints and exits, and a window reload
// doesn't bring it back.
function placeholder(bin: string): vscode.TerminalOptions {
  return { name: PLACEHOLDER_NAME, shellPath: bin, shellArgs: ["version"], env: CLEAN_ENV, hideFromUser: true, isTransient: true };
}

async function openFromProfile(bin: string): Promise<vscode.TerminalProfile> {
  const t = vscode.window.createTerminal(terminalOptions(bin));
  ours.add(t);
  t.show();
  void track(t, bin, true);
  await Promise.race([t.processId, new Promise((r) => setTimeout(r, 5000))]);
  return new vscode.TerminalProfile(placeholder(bin));
}

export function activate(ctx: vscode.ExtensionContext): void {
  workspaceState = ctx.workspaceState;
  known = [...tabbed()];
  bar = vscode.window.createStatusBarItem("reminal.status", vscode.StatusBarAlignment.Left, 50);
  bar.name = "reminal";
  bar.command = "reminal.goToTerminal";
  ctx.subscriptions.push(
    bar,
    { dispose: () => timer && clearInterval(timer) },
    vscode.window.registerTerminalProfileProvider(PROFILE_ID, {
      provideTerminalProfile() {
        const bin = resolveBinary();
        if (!bin) {
          void notInstalled();
          // Throwing makes the editor drop the request instead of opening a
          // terminal that would only fail to start.
          throw new Error("reminal is not installed");
        }
        return openFromProfile(bin);
      },
    }),
    vscode.window.onDidOpenTerminal((t) => {
      known = [...tabbed()];
      if ((t.creationOptions as vscode.TerminalOptions).hideFromUser) return;
      const bin = resolveBinary();
      if (!ours.has(t) && bin && attachedId((t.creationOptions as vscode.TerminalOptions).shellArgs)) {
        void adopt(bin); // a terminal viewing a session, brought back by a reload
      } else if (!ours.has(t) && bin && isReminalTerminal(t, bin)) {
        ours.add(t);
        void track(t, bin, true);
      } else if (!ours.has(t) && bin) {
        void adopt(bin); // perhaps a restored reminal terminal
      }
    }),
    vscode.window.registerFileDecorationProvider(decorations),
    vscode.window.onDidChangeActiveTerminal((t) => t && syncName(t)),
    decorationsChanged,
    vscode.window.onDidCloseTerminal((t) => {
      shownStatus.delete(t);
      // One hidden from the user had no tab and no place in the order.
      if (!(t.creationOptions as vscode.TerminalOptions).hideFromUser) order.closed(known.indexOf(t), known.length);
      known = [...tabbed()];
      decorationsChanged.fire(undefined);
      sessions.delete(t);
      const vp = viewerPid.get(t);
      if (vp) setTimeout(() => setViewer(vp, undefined), FORGET_VIEWER_MS);
      schedule();
    }),
    vscode.commands.registerCommand("reminal.terminals", cloudButton),
    vscode.commands.registerCommand("reminal.newHere", () => openReminal(cwdOf(vscode.window.activeTerminal))),
    vscode.commands.registerCommand("reminal.shareForPerson", (arg: unknown) => share(arg, "person")),
    vscode.commands.registerCommand("reminal.shareForAgent", (arg: unknown) => share(arg, "agent")),
    vscode.commands.registerCommand("reminal.openInFolder", openInFolder),
    vscode.commands.registerCommand("reminal.goToTerminal", goToTerminal),
    vscode.commands.registerCommand("reminal.copyItem", (arg: unknown, many: unknown) => copyItem(deps, arg, many)),
    vscode.commands.registerCommand("reminal.pasteHere", (arg: unknown) => pasteHere(deps, arg)),
    vscode.commands.registerCommand("reminal.sharePort", () => (resolveBinary() ? sharePort(deps) : notInstalled())),
    vscode.commands.registerCommand("reminal.showLink", async (arg: unknown) => {
      const t = terminalArg(arg);
      const s = t && sessions.get(t);
      if (s && stopped.has(t)) return notSharing(t, s);
      if (s) return showLink(s);
      vscode.window.showInformationMessage(
        "This terminal isn't a reminal session. Use \"Open reminal in This Folder\" to open one there.",
      );
    }),
  );
  const bin = resolveBinary();
  if (bin) void adopt(bin);
}

export function deactivate(): void {
  install?.stop();
}

