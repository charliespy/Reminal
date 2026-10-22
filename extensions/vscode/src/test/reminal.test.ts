// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

import { test } from "node:test";
import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import {
  attachArgs,
  attachedId,
  otherSessions,
  tabName,
  renamedTo,
  copyCodeOf,
  exposedOf,
  findReminal,
  INSTALL_UNIX,
  INSTALL_WINDOWS,
  matchSession,
  parseSessions,
  alreadyExposedOf,
  isWebLink,
  stillShared,
  pasteCode,
  plainText,
  stopArgs,
  problemOf,
  querySessions,
  savedPathOf,
  SessionInfo,
  statusOf,
  summary,
  validPort,
} from "../reminal";
import { shareAgentText } from "../share";

const s = (id: string, pid: number, cwd: string, startedAt: number): SessionInfo => ({
  id, pid, cwd, pin: "123456", open_url: `http://r/s/${id}`, join_url: `http://r/s/${id}#p=123456`,
  started_at: new Date(startedAt).toISOString(),
});

test("parseSessions reads the JSON list and ignores the no-sessions line", () => {
  assert.deepEqual(parseSessions("No reminal shell sessions running.\n"), []);
  assert.deepEqual(parseSessions("garbage"), []);
  const one = s("ABCD2345", 42, "/w", 0);
  assert.deepEqual(parseSessions(JSON.stringify([one]) + "\n"), [one]);
});

test("matchSession: a known pid only ever matches that pid", () => {
  const now = Date.now();
  const list = [s("AAAA2345", 10, "/w", now), s("BBBB2345", 11, "/w", now)];
  assert.equal(matchSession(list, 11, "/w", now)?.id, "BBBB2345");
  // Not registered yet: no guess at a neighbour in the same folder.
  assert.equal(matchSession(list, 12, "/w", now), undefined);
});

test("matchSession without a pid takes the newest fresh session in the folder", () => {
  const now = Date.now();
  const list = [
    s("OLD02345", 1, "/w", now - 60000),
    s("OTHR2345", 2, "/elsewhere", now + 100),
    s("NEW02345", 3, "/w/", now + 200),
  ];
  assert.equal(matchSession(list, undefined, "/w", now)?.id, "NEW02345");
  assert.equal(matchSession([list[0]], undefined, "/w", now), undefined);
});

test("findReminal: setting, then PATH, then the installer's folder", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rem-find-"));
  const home = path.join(dir, "home");
  const onPath = path.join(dir, "bin");
  fs.mkdirSync(path.join(home, ".local", "bin"), { recursive: true });
  fs.mkdirSync(onPath);
  const exe = (p: string) => { fs.writeFileSync(p, "#!/bin/sh\n"); fs.chmodSync(p, 0o755); return p; };
  const installed = exe(path.join(home, ".local", "bin", "reminal"));
  assert.equal(findReminal("", { PATH: "/nonexistent" }, "linux", home), installed);
  const first = exe(path.join(onPath, "reminal"));
  assert.equal(findReminal("", { PATH: onPath }, "linux", home), first);
  assert.equal(findReminal("~/.local/bin/reminal", { PATH: onPath }, "linux", home), installed);
  // A setting that points nowhere is reported, not silently replaced.
  assert.equal(findReminal("/nope/reminal", { PATH: onPath }, "linux", home), undefined);
  fs.rmSync(dir, { recursive: true });
});

test("findReminal only takes absolute paths", () => {
  // A relative PATH entry or setting is looked up from wherever this process
  // happens to be, and the terminal would resolve it from its own folder.
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rem-rel-"));
  const cwd = process.cwd();
  process.chdir(dir);
  try {
    fs.mkdirSync("bin");
    fs.writeFileSync("bin/reminal", "#!/bin/sh\n");
    fs.chmodSync("bin/reminal", 0o755);
    const home = path.join(dir, "home");
    assert.equal(findReminal("", { PATH: "bin" }, "linux", home), undefined);
    assert.equal(findReminal("", { PATH: "." + path.delimiter + "bin" }, "linux", home), undefined);
    assert.equal(findReminal("bin/reminal", { PATH: "" }, "linux", home), undefined);
    assert.equal(findReminal("", { PATH: path.join(dir, "bin") }, "linux", home), path.join(dir, "bin", "reminal"));
  } finally {
    process.chdir(cwd);
    fs.rmSync(dir, { recursive: true });
  }
});

test("statusOf maps attn to the viewer's three states and nothing else", () => {
  assert.equal(statusOf("input"), "needs");
  assert.equal(statusOf("logged-out"), "needs");
  assert.equal(statusOf("working"), "working");
  assert.equal(statusOf("done"), "done");
  // A bare shell, or a reminal too old to report attn: no state, no guess.
  assert.equal(statusOf(undefined), undefined);
  assert.equal(statusOf(""), undefined);
  assert.equal(statusOf("something-new"), undefined);
});

test("summary leads with what needs you", () => {
  assert.deepEqual(summary(["working", "needs", "done", "working"]),
    { text: "$(bell) reminal: 1 needs you, 2 working, 1 done", top: "needs" });
  assert.deepEqual(summary(["working", undefined]), { text: "$(sync~spin) reminal: 1 working", top: "working" });
  assert.deepEqual(summary(["done"]), { text: "$(check) reminal: 1 done", top: "done" });
  // Nothing reports a state (bare shells, or an older reminal): just a count.
  assert.deepEqual(summary([undefined, undefined]), { text: "$(cloud) reminal: 2" });
});

// A stand-in reminal that prints what it is given and exits with a code.
function fakeReminal(out: string, code = 0): string {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "rem-q-"));
  const bin = path.join(dir, "reminal");
  fs.writeFileSync(bin, `#!/bin/sh\ncat <<'EOF'\n${out}\nEOF\nexit ${code}\n`, { mode: 0o755 });
  return bin;
}

test("querySessions tells an empty machine from one it couldn't ask", { skip: process.platform === "win32" }, async () => {
  const one = s("ABCD2345", 42, "/w", 0);
  assert.deepEqual(await querySessions(fakeReminal(JSON.stringify([one]))), [one]);
  assert.deepEqual(await querySessions(fakeReminal("No reminal shell sessions running.")), []);
  // These must not read as "no sessions": Share would then call a live
  // session stopped.
  assert.equal(await querySessions(fakeReminal("unknown option --all", 2)), undefined);
  assert.equal(await querySessions(fakeReminal("some other output")), undefined);
  assert.equal(await querySessions(fakeReminal("[{not json")), undefined);
  assert.equal(await querySessions("/nonexistent/reminal"), undefined);
});

test("the install lines are the ones the share text gives", () => {
  const agent = shareAgentText("ABCD2345", "123456", "http://r");
  assert.ok(agent.includes(INSTALL_UNIX), INSTALL_UNIX);
  assert.ok(agent.includes(INSTALL_WINDOWS), INSTALL_WINDOWS);
});

test("copy, paste and expose output is read the way reminal prints it", () => {
  const copied = "Copy code: ABCDE-FGHJK\nOn the other machine, run:  reminal paste ABCDE-FGHJK\nHolding the folder proj in the background (pid 42, expires in 1h0m0s).\n";
  assert.equal(copyCodeOf(copied), "ABCDE-FGHJK");
  assert.equal(copyCodeOf("error: no such file"), undefined);
  for (const typed of ["ABCDE-FGHJK", "abcdefghjk", " abcde fghjk ", "reminal paste ABCDE-FGHJK", "REMINAL PASTE abcdefghjk ~/x"]) {
    assert.equal(pasteCode(typed), "ABCDE-FGHJK", typed);
  }
  assert.equal(pasteCode("ABCD-EFGH"), "ABCDEFGH"); // an older reminal's code: reminal itself explains
  for (const bad of ["", "ABC", "ABCDE-FGHJKL", "reminal paste", "not a code", "abc de fghjk", "ABCDE-FGHJ"]) {
    assert.equal(pasteCode(bad), undefined, bad);
  }
  assert.equal(savedPathOf("Saved /home/me/proj (2)\n"), "/home/me/proj (2)");
  assert.equal(savedPathOf("code is either too old or invalid"), undefined);
  const exposed = "\n  reminal - exposing localhost:8080 | PIN-protected | v3\n\n  Public URL:  https://port-9sc7gbt6.reminal.app/\n  PIN:         310346\n  Quick link:  https://port-9sc7gbt6.reminal.app/#p=310346   (one-tap auth for you)\n";
  assert.deepEqual(exposedOf(exposed), {
    url: "https://port-9sc7gbt6.reminal.app/",
    pin: "310346",
    link: "https://port-9sc7gbt6.reminal.app/#p=310346",
  });
  for (const [v, want] of [["3000", 3000], ["localhost:8080", 8080], [" 65535 ", 65535], ["0", undefined], ["65536", undefined], ["30a", undefined], ["", undefined]] as const) {
    assert.equal(validPort(v), want, v);
  }
  assert.equal(problemOf({ ok: false, stdout: "", stderr: "error: folder too large (max 100.0 MB)\n" }), "folder too large (max 100.0 MB)");
});

test("a paste error can't put a link in a notification", () => {
  // reminal's error can carry a name chosen on the other machine.
  const r = { ok: false, stdout: "", stderr: 'error: the copied folder has an entry outside it: "[Open](command:workbench.action.terminal.sendSequence)"\n' };
  const shown = problemOf(r);
  assert.ok(!/\[[^\]]*\]\(/.test(shown), shown);
  assert.ok(shown.includes("Open(command:"), shown);
  assert.equal(plainText("a\u001b[2Jb\u202ec[d](e)"), "a2Jbcd(e)");
  assert.equal(plainText("x".repeat(10), 4), "xxxx…");
  assert.equal(problemOf({ ok: false, stdout: "", stderr: "", timedOut: true }), "it took too long and was stopped.");
});

test("Stop sharing names the forward, not its port, and only one still shared", () => {
  const s = { id: "ab12cd34", pin: "123456", open_url: "https://x", port: 3000, kind: "port" } as Parameters<typeof stopArgs>[0];
  assert.deepEqual(stopArgs(s), ["stop", "--yes", "--", "ab12cd34"]);
  assert.equal(stopArgs({ ...s, id: "-x" }), undefined);
  assert.equal(stopArgs({ ...s, id: "ab 12" }), undefined);
  // Gone, or now a shell session with that id: not stopped.
  assert.equal(stillShared([], "ab12cd34"), undefined);
  assert.equal(stillShared([{ ...s, kind: undefined, port: undefined }], "ab12cd34"), undefined);
  assert.equal(stillShared([s], "ab12cd34"), s);
});

test("a copy code is shown only in the shape reminal hands out", () => {
  assert.equal(copyCodeOf("Copy code: ABCDE-FGHJK\n"), "ABCDE-FGHJK");
  assert.equal(copyCodeOf("Copy code: ABCD-EFGH\n"), "ABCD-EFGH");
  for (const bad of ["Copy code: [x](command:y)\n", "Copy code: ABCDE-FGHJK extra\n", "Copy code: abcde-fghjk\n"]) {
    assert.equal(copyCodeOf(bad), undefined, bad);
  }
});

test("an already shared port and the links handed to the browser", () => {
  assert.equal(alreadyExposedOf("Port 3000 is already exposed: https://port-ab12cd34.reminal.app/\nTo replace it: ..."), "https://port-ab12cd34.reminal.app/");
  assert.equal(alreadyExposedOf("Public URL: https://x"), undefined);
  assert.ok(isWebLink("https://port-ab12cd34.reminal.app/#p=1") && isWebLink("http://127.0.0.1:18080/?s=X"));
  for (const bad of ["command:workbench.action.terminal.sendSequence", "file:///etc/passwd", "vscode://x", "", undefined]) {
    assert.ok(!isWebLink(bad), String(bad));
  }
});

test("attachArgs opens a session by its exact id, and nothing that isn't an id", () => {
  assert.deepEqual(attachArgs("QFFN9H95"), ["attach", "QFFN9H95"]);
  assert.equal(attachArgs("--all"), undefined);
  assert.equal(attachArgs("QFF N9"), undefined);
  assert.equal(attachArgs(""), undefined);
});

test("attachedId reads back the session a viewing terminal was opened on", () => {
  assert.equal(attachedId(["attach", "ABC123"]), "ABC123");
  assert.equal(attachedId(["version"]), undefined);
  assert.equal(attachedId(["attach"]), undefined);
  assert.equal(attachedId(["attach", "-x"]), undefined);
  assert.equal(attachedId("attach ABC123"), undefined);
  assert.equal(attachedId(undefined), undefined);
});

test("otherSessions: shells not shown here, newest first, without ports or the editor's own", () => {
  const port = { ...s("PORT1", 9, "/p", 5000), kind: "port", port: 3000 };
  const list = [s("OLD", 1, "/a", 1000), s("SHOWN", 2, "/b", 2000), s("NEW", 3, "/c", 3000), s("SELF", 4, "/d", 4000), port];
  assert.deepEqual(otherSessions(list, new Set(["SHOWN"]), "SELF").map((x) => x.id), ["NEW", "OLD"]);
  assert.deepEqual(otherSessions(list, new Set(), undefined).map((x) => x.id), ["SELF", "NEW", "SHOWN", "OLD"]);
});

test("tabName: one plain line, cut short, and nothing for no name", () => {
  assert.equal(tabName("build server"), "build server");
  assert.equal(tabName("  two\nlines\u202e  "), "two lines");
  assert.equal(tabName("x".repeat(80)), "x".repeat(60) + "\u2026");
  assert.equal(tabName(""), undefined);
  assert.equal(tabName("   "), undefined);
  assert.equal(tabName(undefined), undefined);
});

test("renamedTo: a tab renamed by hand names its session, nothing else does", () => {
  const defaults = ["reminal", "reminal.exe"];
  const r = (tab: string, seen: string | undefined, given?: string, session?: string) =>
    renamedTo({ tab, seen, given, session, defaults });
  // Renamed by hand since the last look.
  assert.equal(r("C3PRICING-501", "reminal"), "C3PRICING-501");
  assert.equal(r("new name", "old name", "old name", "old name"), "new name");
  assert.equal(r("  two\nlines ", "reminal"), "two lines");
  assert.equal(r("y".repeat(80), "reminal"), "y".repeat(80));
  // Unchanged, or a name this extension gave it or the session already has.
  assert.equal(r("C3PRICING-501", "C3PRICING-501"), undefined);
  assert.equal(r("from-session", "reminal", "from-session", "from-session"), undefined);
  assert.equal(r("x".repeat(60) + "\u2026", "reminal", undefined, "x".repeat(80)), undefined);
  // The name it was given before the session was renamed elsewhere, while
  // the tab was not the active one: not taken back.
  assert.equal(r("old", "old", "old", "renamed-elsewhere"), undefined);
  // A default name, or none.
  assert.equal(r("reminal", "something"), undefined);
  assert.equal(r("reminal.exe", undefined), undefined);
  assert.equal(r("   ", "reminal"), undefined);
  // First look: passed on only to a session with no name.
  assert.equal(r("C3PRICING-501", undefined), "C3PRICING-501");
  assert.equal(r("C3PRICING-501", undefined, undefined, "named-since"), undefined);
});
