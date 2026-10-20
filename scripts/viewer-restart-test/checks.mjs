// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// run.sh: a page left open on a session whose process ends and comes back as
// itself. Asks run.sh for each restart by printing RESTART, and waits for its
// "go" on stdin.
import { chromium } from 'playwright';
import readline from 'node:readline';
const BASE = 'http://127.0.0.1:18080';
const [S, PIN] = process.argv.slice(2);
const lines = readline.createInterface({ input: process.stdin })[Symbol.asyncIterator]();
const restart = async () => { console.log('RESTART'); await lines.next(); };
const b = await chromium.launch();
let failed = 0;
const verdict = (ok) => { if (!ok) failed++; return ok ? 'PASS' : 'FAIL'; };

const ctx = await b.newContext({ viewport: { width: 1000, height: 700 } });
await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
const p = await ctx.newPage();
await p.goto(`${BASE}/?s=${S}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
const live = () => p.waitForFunction(() => inputLive(), null, { timeout: 60000 }).then(() => true, () => false);
// A step the rest cannot go on without: said as a FAIL, and the run ended
// with it, never a stack trace.
const mustOr = async (ok, what) => {
  if (ok) return;
  console.log(`${what}   FAIL`);
  await b.close();
  process.exit(1);
};
await mustOr(await live(), '0  the page has the session live before any restart');
const screen = () => p.evaluate(() => {
  const b = term.buffer.active, out = [];
  for (let i = 0; i < b.length; i++) out.push(b.getLine(i).translateToString(true));
  return out.join('\n');
});
const typeLine = async (s) => { await p.locator('.xterm-helper-textarea').focus(); await p.keyboard.type(s, { delay: 15 }); await p.keyboard.press('Enter'); };
const shows = (re, ms) => p.waitForFunction((src) => {
  const b = term.buffer.active;
  for (let i = 0; i < b.length; i++) if (new RegExp(src).test(b.getLine(i).translateToString(true))) return true;
  return false;
}, re.source, { timeout: ms }).then(() => true, () => false);

const restarted = async () => {
  await restart();
  // Away, then back: the page dials and hands the session's key again.
  await p.waitForFunction(() => !agentLive || !cryptoKey, null, { timeout: 30000 }).catch(() => {});
  await mustOr(await live(), '   the session is live on the page again after its restart');
};
const echoes = async (n, what) => {
  await p.keyboard.press('Control+U');
  await typeLine(`echo after-$((${n}*1000+7))`);
  const ok = await shows(new RegExp(`^after-${n * 1000 + 7}$`), 30000);
  console.log(`${n}  ${what}   ${verdict(ok)}`);
  if (!ok) console.log((await screen()).split('\n').filter(Boolean).slice(-5).map(l => '     | ' + l).join('\n'));
};

// BIG=1: only this — a long scrollback, restarted: how long the page waits
// for its key while the restored session replays it.
if (process.env.BIG) {
  await typeLine('seq 1 300000; echo seq-done');
  await shows(/^seq-done$/, 600000);
  console.log('SAVED'); await lines.next();
  await p.evaluate(() => {
    window.__liveAt = 0; window.__offAt = 0;
    setInterval(() => {
      if (!agentLive) { if (!window.__offAt) window.__offAt = Date.now(); }
      else if (window.__offAt && !window.__liveAt && inputLive()) window.__liveAt = Date.now();
    }, 20);
  });
  await restarted();
  await shows(/reminal restored this session/, 600000);
  const drawnAt = Date.now();
  const { off, live } = await p.evaluate(() => ({ off: window.__offAt, live: window.__liveAt }));
  const iso = (t) => new Date(t).toISOString().slice(11, 23);
  console.log(`BIG  page: offline ${iso(off)}, has its key ${iso(live)}, restored screen drawn ${iso(drawnAt)} (UTC)`);
  await b.close();
  process.exit(0);
}

// 1: back from a record as new as the screen: its count goes on from the page's.
await typeLine('echo before-1');
await shows(/^before-1$/, 20000);
await restarted();
await echoes(1, 'restarted from a record as new as what the page showed: what is typed echoes');

// 2: back from a record older than the screen by a few keys' echo — what it
// said after its last save, lost with the process: its count starts again
// below the page's, by less than the page took for a restart.
await typeLine('echo before-2');
await shows(/^before-2$/, 20000);
console.log('SAVED'); await lines.next();
const before = await p.evaluate(() => lastSeq);
await p.keyboard.type('xyz', { delay: 50 });
await shows(/xyz$/, 20000);
const typed = await p.evaluate(() => lastSeq);
await restarted();
await echoes(2, `restarted from a record ${typed - before} frame(s) older than the page's screen: what is typed echoes`);

// 3: and its screen is the one it came back with, not the page's old one.
const banner = await shows(/reminal restored this session/, 5000);
console.log(`3  its screen is drawn again after it came back   ${verdict(banner)}`);

await b.close();
console.log(failed ? `${failed} FAILED` : 'all passed');
process.exit(failed ? 1 : 0);
