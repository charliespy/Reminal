// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

import { test } from "node:test";
import * as assert from "node:assert/strict";
import { TabOrder } from "../tabs";

test("tabs asked about before all are seen get no answer, then are asked again", () => {
  // A window reload: the tab list asks about every tab in one go, newest
  // first, before the extension knows them all.
  const o = new TabOrder<string>();
  assert.deepEqual(o.ask(7, "u7", 3), { again: false });
  assert.deepEqual(o.ask(5, "u5", 3), { again: false });
  assert.deepEqual(o.ask(6, "u6", 3), { index: 1, again: true });
  // The second round, after the "ask again": every tab gets its place.
  assert.deepEqual(o.ask(7, "u7", 3), { index: 2, again: false });
  assert.deepEqual(o.ask(5, "u5", 3), { index: 0, again: false });
  assert.equal(o.uriAt(0, 3), "u5");
  assert.equal(o.uriAt(2, 3), "u7");
});

test("all tabs seen in one go needs no second round", () => {
  const o = new TabOrder<string>();
  assert.deepEqual(o.ask(1, "u1", 1), { index: 0, again: false });
});

test("a closed tab's id never counts again", () => {
  const o = new TabOrder<string>();
  for (const id of [1, 2, 3]) o.ask(id, `u${id}`, 3);
  o.closed(1, 3); // the middle one
  // The decoration service asks about the closed URI again: no answer, and it
  // must not throw the others off.
  assert.deepEqual(o.ask(2, "u2", 2), { again: false });
  assert.equal(o.ask(1, "u1", 2).index, 0);
  assert.equal(o.ask(3, "u3", 2).index, 1);
  assert.equal(o.uriAt(1, 2), "u3");
});

test("when the closed one can't be placed, every id is learnt again", () => {
  const o = new TabOrder<string>();
  o.ask(1, "u1", 2); // only one of two seen
  o.closed(0, 2);
  assert.equal(o.uriAt(0, 1), undefined);
  assert.deepEqual(o.ask(2, "u2", 1), { index: 0, again: true });
});

test("no answer, never a wrong one, while the counts disagree", () => {
  const o = new TabOrder<string>();
  for (const id of [1, 2, 3]) o.ask(id, `u${id}`, 3);
  // A fourth terminal opened but not drawn yet.
  assert.equal(o.ask(2, "u2", 4).index, undefined);
  assert.equal(o.uriAt(1, 4), undefined);
  assert.equal(o.uriAt(-1, 3), undefined);
});

test("a seen tab closed while a newer one is still unseen is told by its place", () => {
  // Three tabs seen; a fourth opens while the panel is hidden; then the first
  // closes. When the panel shows again the decoration service asks about the
  // closed id too, and that must not shift the others.
  const o = new TabOrder<string>();
  for (const id of [1, 2, 3]) o.ask(id, `u${id}`, 3);
  o.closed(0, 4); // terminals were 1, 2, 3, 4 (4 never drawn)
  assert.equal(o.ask(1, "u1", 3).index, undefined);
  assert.equal(o.ask(2, "u2", 3).index, undefined);
  assert.equal(o.ask(3, "u3", 3).index, undefined); // not "2": that is terminal 4
  assert.equal(o.uriAt(1, 3), undefined);
  assert.deepEqual(o.ask(4, "u4", 3), { index: 2, again: true });
  assert.equal(o.ask(3, "u3", 3).index, 1);
  assert.equal(o.ask(1, "u1", 3).index, undefined); // dead for good
  assert.equal(o.uriAt(0, 3), "u2");
});

test("an unseen tab that closes leaves the seen ones as they were", () => {
  const o = new TabOrder<string>();
  for (const id of [1, 2, 3]) o.ask(id, `u${id}`, 3);
  o.closed(3, 4); // the fourth, never drawn, closed
  // Every id is learnt again as the tabs are redrawn.
  assert.equal(o.ask(1, "u1", 3).index, undefined);
  assert.equal(o.ask(2, "u2", 3).index, undefined);
  assert.deepEqual(o.ask(3, "u3", 3), { index: 2, again: true });
  assert.equal(o.uriAt(1, 3), "u2");
});
