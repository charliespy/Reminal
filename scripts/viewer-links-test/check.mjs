// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// run.sh's container. A forwarded app opened from the Machines panel
// (openPortOnMachine) is opened in a tab that has no way back to the page
// that opened it: its window.opener is null, and the page is served with a
// Cross-Origin-Opener-Policy.
//   L1  the opened tab's window.opener is null;
//   L2  the viewer page is served with Cross-Origin-Opener-Policy:
//       same-origin-allow-popups.
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { chromium } from 'playwright';

let fails = 0;
const result = (id, what, ok, why) => { console.log(`${ok ? 'PASS' : 'FAIL'} ${id}: ${what}${ok ? '' : '  — ' + why}`); if (!ok) fails++; };
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function up(f, secs) { for (let i = 0; i < secs * 4; i++) { try { if (await f()) return true; } catch (_) {} await sleep(250); } return false; }

// The forwarded app: on another origin, it says whether it has an opener.
createServer((q, r) => { r.writeHead(200, { 'content-type': 'text/html' }); r.end('<!doctype html><title>app</title><script>document.title = "opener:" + (window.opener === null ? "none" : "some")</script>'); }).listen(8099);
spawn('reminal', ['relay', '8080'], { detached: true, stdio: 'ignore' }).unref();
await up(async () => (await fetch('http://127.0.0.1:8080/')).ok, 30);
const head = await fetch('http://127.0.0.1:8080/');
const coop = head.headers.get('cross-origin-opener-policy') || '';
result('L2', 'the viewer page is served with Cross-Origin-Opener-Policy: same-origin-allow-popups', coop === 'same-origin-allow-popups', JSON.stringify(coop));

const browser = await chromium.launch();
const ctx = await browser.newContext();
const page = await ctx.newPage();
await page.goto('http://127.0.0.1:8080/');
await page.waitForFunction(() => typeof openPortOnMachine === 'function', null, { timeout: 30000 });
// The machine's answer: the forwarded app's link (as a machine gives it).
await page.evaluate(() => {
  queryDirectory = async () => ({ open_url: 'http://127.0.0.1:8099/' });
  const b = document.createElement('button'); b.id = 'go'; b.textContent = 'open'; b.style.cssText = 'position:fixed;top:0;left:0;z-index:99999';
  b.onclick = () => openPortOnMachine('AAAA', 'PORT1234', 8099);
  document.body.appendChild(b);
});
const [tab] = await Promise.all([ctx.waitForEvent('page'), page.click('#go')]);
await tab.waitForFunction(() => /^opener:/.test(document.title), null, { timeout: 15000 }).catch(() => {});
const title = await tab.title();
result('L1', 'the forwarded app opened from the viewer has no opener', title === 'opener:none', title);
await browser.close();
console.log(fails ? `${fails} failed` : 'all passed');
process.exit(fails ? 1 : 0);
