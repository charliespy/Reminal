// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

import { test } from "node:test";
import * as assert from "node:assert/strict";
import * as fs from "node:fs";
import * as path from "node:path";
import * as vm from "node:vm";
import { shareAgentText, shareHumanText } from "../share";

// The web viewer's app.js, from the repo this extension lives in.
const APP_JS = path.resolve(__dirname, "../../../../internal/client/web/app.js");

// source cuts one top-level function out of app.js by matching its braces.
function source(js: string, name: string): string {
  const start = js.indexOf(`function ${name}(`);
  assert.ok(start >= 0, `app.js has no function ${name}`);
  let depth = 0;
  for (let i = js.indexOf("{", start); i < js.length; i++) {
    if (js[i] === "{") depth++;
    else if (js[i] === "}" && --depth === 0) return js.slice(start, i + 1);
  }
  throw new Error(`unbalanced function ${name}`);
}

// viewerTexts runs the viewer's own share functions for a session.
function viewerTexts(id: string, pin: string, origin: string): { human: string; agent: string } {
  const js = fs.readFileSync(APP_JS, "utf8");
  const fns = ["sharePin", "shareJoinURL", "shareHumanText", "shareAgentText"].map((n) => source(js, n)).join("\n");
  const ctx = { sessionId: id.toLowerCase(), pinValue: pin, sharePinFromAgent: null, location: { origin }, out: {} };
  vm.runInNewContext(`${fns}\nout = { human: shareHumanText(), agent: shareAgentText() };`, ctx);
  return ctx.out as { human: string; agent: string };
}

test("the share texts are the web viewer's, word for word", () => {
  const origin = "https://live.reminal.app";
  const id = "AB12CD34";
  const pin = "482916";
  // What `reminal info --json` gives as join_url: open_url (WEB/?s=ID) + #p=PIN.
  const url = `${origin}/?s=${id}#p=${pin}`;
  const viewer = viewerTexts(id, pin, origin);
  assert.equal(shareHumanText(id, pin, url), viewer.human);
  assert.equal(shareAgentText(id, pin, url), viewer.agent);
});

test("the texts carry the session's own id and PIN", () => {
  const t = shareHumanText("ab12cd34", "482916", "u");
  assert.match(t, /reminal connect AB12CD34 482916/);
  assert.match(shareAgentText("AB12CD34", "482916", "u"), /^ {2}reminal connect AB12CD34 482916$/m);
});
