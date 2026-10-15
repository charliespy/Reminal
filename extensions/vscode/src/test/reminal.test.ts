// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

import { test } from "node:test";
import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import {
  findReminal,
  INSTALL_UNIX,
  INSTALL_WINDOWS,
  matchSession,
  parseSessions,
  querySessions,
  SessionInfo,
  statusOf,
  summary,
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
