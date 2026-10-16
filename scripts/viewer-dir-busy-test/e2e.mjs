// e2e.sh: two browsers hammering one machine past its allowance.
import { chromium } from 'playwright';
import { execSync, spawn } from 'node:child_process';

let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const check = (c, good, why) => (c ? ok(good) : fail(good + ' — ' + why));
const sh = (c) => execSync(c, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
async function until(fn, secs = 30) {
  for (const end = Date.now() + secs * 1000; Date.now() < end; await sleep(300)) { try { if (await fn()) return true; } catch (_) {} }
  return false;
}

spawn('reminal', ['relay', '8080'], { detached: true, stdio: 'ignore' }).unref();
if (!(await until(async () => (await fetch('http://127.0.0.1:8080/')).ok, 20))) { fail('the relay did not start'); process.exit(1); }
const browser = await chromium.launch();
// Two browsers (two owner devices), each a page on the machine's relay.
const pages = [];
for (let i = 0; i < 2; i++) {
  const ctx = await browser.newContext();
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
  const p = await ctx.newPage();
  p.errs = [];
  p.on('pageerror', e => p.errs.push(e.message));
  p.on('dialog', d => d.accept());
  await p.goto('http://127.0.0.1:8080/');
  await p.waitForFunction(() => typeof ownDeviceId === 'function' && typeof queryDirectory === 'function');
  sh(`reminal add owner ${await p.evaluate(() => ownDeviceId())} -y`);
  pages.push(p);
}
spawn('reminal', ['daemon'], { detached: true, stdio: 'ignore' }).unref();
await sleep(1500);
const sid = sh('reminal new first').match(/Session:\s+([A-Z0-9]+)/)[1];
for (const p of pages) {
  await p.evaluate(s => connectOwned(s), sid);
  check(await until(() => p.evaluate(async () => Object.keys(await loadOwnedMachines()).length === 1), 40), 'a browser owns the machine', 'no machine');
  await p.evaluate(() => userDisconnect());
}

// Both at once: 30 listings and 6 new sessions each — past the machine's
// burst of 32 queries and 4 sessions.
const results = await Promise.all(pages.map(p => p.evaluate(async () => {
  const key = Object.keys(await loadOwnedMachines())[0];
  const pub = base64ToU8(b64URLToStd(key));
  const calls = [];
  for (let i = 0; i < 30; i++) calls.push(queryDirectory(pub, { timeout: 6000 }).then(r => Array.isArray(r.sessions) ? 'ok' : 'no-list', e => 'ERR ' + e.message));
  for (let i = 0; i < 6; i++) calls.push(queryDirectory(pub, { spawn: true, spawnName: 'h' + i, spawnCwd: '', timeout: 20000 }).then(r => r && r.id ? 'spawned' : 'no-id', e => 'ERR ' + e.message));
  return Promise.all(calls);
})));
const all = results.flat();
const tally = all.reduce((m, r) => (m[r] = (m[r] || 0) + 1, m), {});
check(all.length === 72 && tally.ok === 60 && tally.spawned === 12, 'every request answered: 60 listings, and 12 sessions started (each with its id)', JSON.stringify(tally));
check(!all.some(r => /timeout|unreachable/.test(r)), 'none read as the machine offline', JSON.stringify(tally));
for (const p of pages) check(!p.errs.length, 'no page errors', p.errs.join(' | '));
await browser.close();
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
