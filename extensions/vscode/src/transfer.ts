// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// reminal Copy and reminal Paste in the Explorer's right-click menu, and the
// ports reminal shares (`reminal expose`) in the cloud button's list.

import * as fs from "node:fs";
import * as path from "node:path";
import * as vscode from "vscode";
import {
  SessionInfo,
  alreadyExposedOf,
  copyCodeOf,
  exposedOf,
  isWebLink,
  pasteCode,
  plainText,
  problemOf,
  queryAll,
  runReminal,
  savedPathOf,
  stillShared,
  stopArgs,
  validPort,
} from "./reminal";

export interface Deps {
  // The reminal program, or undefined (after saying how to install it).
  binary: () => string | undefined;
}

// A folder snapshot or a 100 MB file can take a while to pack; the copy
// itself waits in the background, so this is only until the code is ready.
const COPY_TIMEOUT_MS = 2 * 60 * 1000;
// A paste lasts as long as the transfer does.
const PASTE_TIMEOUT_MS = 15 * 60 * 1000;
const EXPOSE_TIMEOUT_MS = 60 * 1000;

// localPath is the file a menu was opened on, when it is one on this side.
function localPath(arg: unknown): string | undefined {
  if (!(arg instanceof vscode.Uri)) return undefined;
  if (arg.scheme !== "file" && arg.scheme !== "vscode-remote") return undefined;
  return arg.fsPath;
}

function plainName(p: string): string {
  return plainText(path.basename(p) || p, 200);
}

// copyItem runs `reminal copy` on the file or folder the menu was opened on
// and shows the one-time code to paste it with elsewhere.
export async function copyItem(deps: Deps, arg: unknown, many?: unknown): Promise<void> {
  if (Array.isArray(many) && many.length > 1) {
    vscode.window.showInformationMessage("reminal copies one file or folder at a time. Pick just one, or put them in a folder.");
    return;
  }
  const p = localPath(arg);
  if (!p || !fs.existsSync(p)) {
    vscode.window.showErrorMessage("reminal can only copy a file or folder on this machine.");
    return;
  }
  const bin = deps.binary();
  if (!bin) return;
  const name = plainName(p);
  const r = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: `Getting ${name} ready to copy…`, cancellable: true },
    (_, token) => runReminal(bin, ["copy", p], COPY_TIMEOUT_MS, undefined, abortOn(token)),
  );
  if (r.cancelled) return;
  const code = r.ok ? copyCodeOf(r.stdout) : undefined;
  if (!code) {
    const why = problemOf(r);
    const tooOld = /directories aren't supported/.test(why);
    vscode.window.showErrorMessage(
      tooOld ? "Copying a folder needs a newer reminal. Run `reminal upgrade`, then try again." : `reminal couldn't copy ${name}: ${why}`,
    );
    return;
  }
  const line = `reminal paste ${code}`;
  const choice = await vscode.window.showInformationMessage(
    `${name} is ready to paste with code ${code}. Use reminal Paste on the other machine, or run "${line}" there. The code works once, within an hour.`,
    "Copy code",
    "Copy paste command",
  );
  if (choice === "Copy code") await vscode.env.clipboard.writeText(code);
  else if (choice === "Copy paste command") await vscode.env.clipboard.writeText(line);
  if (choice) vscode.window.setStatusBarMessage("Copied", 3000);
}

// pasteHere asks for a code from `reminal copy` and pastes what it names
// into the folder the menu was opened on.
export async function pasteHere(deps: Deps, arg: unknown): Promise<void> {
  const dir = localPath(arg);
  if (!dir || !fs.statSync(dir, { throwIfNoEntry: false })?.isDirectory()) {
    vscode.window.showErrorMessage("reminal pastes into a folder on this machine.");
    return;
  }
  const typed = await vscode.window.showInputBox({
    title: `reminal Paste into ${plainName(dir)}`,
    prompt: "The code from reminal Copy or `reminal copy` on the other machine",
    placeHolder: "ABCDE-FGHJK",
    ignoreFocusOut: true,
    validateInput: (v) => (v.trim() === "" || pasteCode(v) ? undefined : "A copy code has 10 letters and numbers, like ABCDE-FGHJK."),
  });
  const code = typed ? pasteCode(typed) : undefined;
  if (!code) return;
  const bin = deps.binary();
  if (!bin) return;
  const r = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: `Pasting into ${plainName(dir)}…`, cancellable: true },
    (_, token) => runReminal(bin, ["paste", code, dir], PASTE_TIMEOUT_MS, dir, abortOn(token)),
  );
  const saved = r.ok ? savedPathOf(r.stdout) : undefined;
  if (!saved) {
    vscode.window.showErrorMessage(
      r.cancelled
        ? `reminal Paste was stopped. Part of it may already be in ${plainName(dir)}, so check that folder.`
        : r.timedOut
          ? `reminal Paste was stopped after ${PASTE_TIMEOUT_MS / 60000} minutes. Part of it may already be in ${plainName(dir)}, so check that folder.`
          : `reminal couldn't paste: ${problemOf(r)}`,
    );
    return;
  }
  const uri = vscode.Uri.file(saved);
  void vscode.commands.executeCommand("revealInExplorer", uri);
  const isFile = fs.statSync(saved, { throwIfNoEntry: false })?.isFile();
  const choice = await vscode.window.showInformationMessage(`Pasted ${plainName(saved)} into ${plainName(dir)}.`, ...(isFile ? ["Open"] : []));
  if (choice === "Open") void vscode.commands.executeCommand("vscode.open", uri);
}

// abortOn turns a progress notification's Cancel into a signal that stops
// the reminal command.
function abortOn(token: vscode.CancellationToken): AbortSignal {
  const ac = new AbortController();
  token.onCancellationRequested(() => ac.abort());
  return ac.signal;
}

function openLink(u: string | undefined): void {
  if (isWebLink(u)) void vscode.env.openExternal(vscode.Uri.parse(u));
}

// ---- shared ports ----

export type PortItem = vscode.QuickPickItem & { port?: SessionInfo; sharePort?: boolean };

// portItems are the cloud list's rows for the ports reminal shares here.
export function portItems(ports: SessionInfo[]): PortItem[] {
  if (!ports.length) return [];
  return [
    { label: "Shared ports", kind: vscode.QuickPickItemKind.Separator },
    ...ports.map((s) => ({
      port: s,
      label: `$(globe) localhost:${s.port}`,
      description: [s.name, s.id.toUpperCase()].filter((x): x is string => !!x).map((x) => plainText(x, 100)).join(" · "),
      detail: plainText(s.open_url),
    })),
  ];
}

export const SHARE_PORT_ITEM: PortItem = { label: "$(plug) Share a Port…", sharePort: true };

// portActions offers what can be done with a shared port.
export async function portActions(deps: Deps, s: SessionInfo): Promise<void> {
  const choice = await vscode.window.showInformationMessage(
    `localhost:${s.port} is shared at ${plainText(s.open_url)}. PIN ${plainText(s.pin, 12)}.`,
    "Copy link",
    "Open in browser",
    "Stop sharing",
  );
  if (choice === "Copy link") {
    await vscode.env.clipboard.writeText(s.join_url || s.open_url);
    vscode.window.setStatusBarMessage("reminal link copied", 3000);
  } else if (choice === "Open in browser") {
    openLink(s.join_url || s.open_url);
  } else if (choice === "Stop sharing") {
    const bin = deps.binary();
    if (!bin) return;
    // The notification may have sat a while: stop it only if that forward
    // is still the one sharing this port.
    const args = stillShared(await queryAll(bin), s.id) ? stopArgs(s) : undefined;
    if (!args) {
      vscode.window.showInformationMessage(`localhost:${s.port} isn't shared any more.`);
      return;
    }
    const r = await runReminal(bin, args, EXPOSE_TIMEOUT_MS);
    if (r.ok) vscode.window.showInformationMessage(`Stopped sharing localhost:${s.port}. Its link no longer works.`);
    else vscode.window.showErrorMessage(`reminal couldn't stop sharing localhost:${s.port}: ${problemOf(r)}`);
  }
}

// sharePort asks for a local port and shares it with `reminal expose`.
export async function sharePort(deps: Deps): Promise<void> {
  const typed = await vscode.window.showInputBox({
    title: "reminal: Share a Port",
    prompt: "A port on this machine, such as your dev server's. Its link asks for a PIN. Unlike terminals, a shared port isn't end-to-end encrypted.",
    placeHolder: "3000",
    validateInput: (v) => (v.trim() === "" || validPort(v) ? undefined : "A port is a number from 1 to 65535."),
  });
  const port = typed ? validPort(typed) : undefined;
  if (!port) return;
  const bin = deps.binary();
  if (!bin) return;
  const r = await vscode.window.withProgress(
    { location: vscode.ProgressLocation.Notification, title: `Sharing localhost:${port}…` },
    () => runReminal(bin, ["expose", String(port)], EXPOSE_TIMEOUT_MS),
  );
  const { url, pin, link } = exposedOf(r.stdout);
  const already = r.ok && !url ? alreadyExposedOf(r.stdout) : undefined;
  if (already) {
    const s = (await queryAll(bin))?.find((x) => x.port === port && x.kind === "port");
    if (s) return portActions(deps, s);
    vscode.window.showInformationMessage(`localhost:${port} is already shared at ${plainText(already)}.`);
    return;
  }
  if (!r.ok || !url) {
    vscode.window.showErrorMessage(`reminal couldn't share localhost:${port}: ${problemOf(r)}`);
    return;
  }
  const choice = await vscode.window.showInformationMessage(
    `localhost:${port} is shared at ${plainText(url)}${pin ? `. PIN ${plainText(pin, 12)}` : ""}.`,
    "Copy link",
    "Open in browser",
  );
  if (choice === "Copy link") {
    await vscode.env.clipboard.writeText(link ?? url);
    vscode.window.setStatusBarMessage("reminal link copied", 3000);
  } else if (choice === "Open in browser") {
    openLink(link ?? url);
  }
}
