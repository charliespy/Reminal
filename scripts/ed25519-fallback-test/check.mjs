// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// run.sh's container: a browser with no Ed25519, as Safari 16 (each Ed25519
// call NotSupportedError), stood in for by Chromium pages whose crypto.subtle
// refuses it so; a page with Ed25519 of its own beside them.
//   E1  the owner key is made all the same, kept as a sealed seed (no key
//       the page holds), and the same device after a reload.
//   E2  a session on a machine it owns connects without a PIN: the machine
//       checks its signature, it checks the machine's.
//   E3  a page with Ed25519 of its own, an owner too, connects the same way.
//   E4  what the no-Ed25519 page signs checks natively, and what the other
//       signs checks there; a signature changed checks nowhere.
//   E5  a native key already kept is never replaced: the same device once
//       Ed25519 is refused.
//   E6  no Ed25519 and no tweetnacl either: a plain message, not the
//       browser's own words.
import { execSync, spawn } from 'node:child_process';
import { openSync } from 'node:fs';
import { chromium } from 'playwright';

let fails = 0;
const check = (ok, what, why) => { console.log((ok ? 'PASS ' : 'FAIL ') + what + (ok ? '' : '  — ' + why)); if (!ok) fails++; };
const sh = (c) => execSync(c, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function until(fn, secs) { for (const end = Date.now() + secs * 1000; Date.now() < end; await sleep(300)) { try { const v = await fn(); if (v) return v; } catch (_) {} } return false; }
spawn('reminal', ['relay', '8080'], { detached: true, stdio: 'ignore' }).unref();
for (let i = 0; i < 100; i++) { try { sh('bash -c "exec 3<>/dev/tcp/127.0.0.1/8080"'); break; } catch (_) { await sleep(100); } }
const BASE = 'http://127.0.0.1:8080/';

// Every Ed25519 call refused, as a browser that has none refuses it.
const NO_ED25519 = () => {
  const s = crypto.subtle, isEd = (a) => (typeof a === 'string' ? a : a && a.name) === 'Ed25519';
  const no = () => Promise.reject(new DOMException('The operation is not supported.', 'NotSupportedError'));
  for (const [f, at] of [['generateKey', 0], ['importKey', 2], ['sign', 0], ['verify', 0]]) {
    const orig = s[f].bind(s);
    s[f] = (...a) => (isEd(a[at]) ? no() : orig(...a));
  }
};
const browser = await chromium.launch();
async function page(noEd, ctx) {
  ctx = ctx || await browser.newContext();
  if (noEd) await ctx.addInitScript(NO_ED25519);
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
  const p = await ctx.newPage();
  p.errs = [];
  p.on('pageerror', (e) => p.errs.push(String(e)));
  p.on('dialog', (d) => (/has not connected to this machine before/.test(d.message()) ? d.accept() : d.dismiss()));
  await p.goto(BASE);
  await p.waitForFunction(() => typeof ownDeviceId === 'function', null, { timeout: 30000 });
  return p;
}
const err = async (f) => { try { return await f(); } catch (e) { return 'ERR ' + String(e && e.message || e); } };
const kindOf = (p) => p.evaluate(() => new Promise((ok) => { const r = indexedDB.open('reminal-own'); r.onsuccess = () => { const g = r.result.transaction('keys', 'readonly').objectStore('keys').get('device'); g.onsuccess = () => { const x = g.result || {}; ok(x.privateKey ? 'key' : x.seed ? 'seed' : 'none'); }; g.onerror = () => ok('error'); }; r.onerror = () => ok('error'); })).catch((e) => 'error ' + e.message);
// Connected to the session as its machine's owner, no PIN: what the page
// says, and whether it holds the session's key.
async function ownedConnect(p, sid) {
  await p.evaluate((sid) => connectOwned(sid), sid);
  const ok = await until(() => p.evaluate(() => !!cryptoKey), 60);
  const status = await p.evaluate(() => (document.getElementById('status') || {}).textContent || '').catch(() => '');
  return { ok: !!ok, status };
}

console.log('E1: an owner key with no Ed25519');
const A = await page(true);
const id1 = await err(() => A.evaluate(() => ownDeviceId()));
const kind = await kindOf(A);
await A.reload(); await A.waitForFunction(() => typeof ownDeviceId === 'function');
const id2 = await err(() => A.evaluate(() => ownDeviceId()));
check(/^rmnl_/.test(id1) && id1 === id2 && kind === 'seed', 'E1: made (' + String(id1).slice(0, 16) + '…), kept as a sealed seed, the same after a reload', JSON.stringify({ id1, id2, kind }));

console.log('E2: a session on a machine it owns, no PIN');
const B = await page(false);
const idB = await B.evaluate(() => ownDeviceId());
let a = { ok: false, status: 'no owner key' }, sid = '';
if (/^rmnl_/.test(id1)) {
  sh(`reminal add owner ${id1} -y`);
  sh(`reminal add owner ${idB} -y`);
  const dlog = openSync('/tmp/daemon.log', 'a');
  spawn('reminal', ['daemon'], { detached: true, stdio: ['ignore', dlog, dlog] }).unref();
  await until(() => { try { sh('reminal list'); return true; } catch (_) { return false; } }, 30);
  sh('mkdir -p /root/w');
  sid = sh('cd /root/w && reminal new probe').match(/Session:\s+([A-Z0-9]+)/)[1];
  a = await ownedConnect(A, sid);
}
check(a.ok, 'E2: connected as its owner, without a PIN', JSON.stringify(a));

console.log('E3: a page with Ed25519 of its own, the same way');
const b = sid ? await ownedConnect(B, sid) : { ok: false, status: 'no session' };
check(b.ok, 'E3: connected as its owner, without a PIN', JSON.stringify(b));

console.log('E4: signatures across the two');
const msg = [1, 2, 3, 4, 5, 6, 7, 8, 9];
const e4 = await err(async () => {
  const fromA = await A.evaluate(async (m) => { const dk = await loadDeviceKeyPair(); return { pub: [...dk.pubRaw], sig: [...await deviceSign(dk, new Uint8Array(m))] }; }, msg);
  const onB = await B.evaluate(async ([s, m]) => {
    const k = await crypto.subtle.importKey('raw', new Uint8Array(s.pub), { name: 'Ed25519' }, false, ['verify']);
    const bad = new Uint8Array(s.sig); bad[0] ^= 1;
    return { ok: await crypto.subtle.verify({ name: 'Ed25519' }, k, new Uint8Array(s.sig), new Uint8Array(m)), bad: await crypto.subtle.verify({ name: 'Ed25519' }, k, bad, new Uint8Array(m)) };
  }, [fromA, msg]);
  const fromB = await B.evaluate(async (m) => { const dk = await loadDeviceKeyPair(); return { pub: [...dk.pubRaw], sig: [...new Uint8Array(await crypto.subtle.sign({ name: 'Ed25519' }, dk.privateKey, new Uint8Array(m)))] }; }, msg);
  const onA = await A.evaluate(async ([s, m]) => {
    const bad = new Uint8Array(s.sig); bad[5] ^= 1;
    return { ok: await ed25519Verify(new Uint8Array(s.pub), new Uint8Array(s.sig), new Uint8Array(m)), bad: await ed25519Verify(new Uint8Array(s.pub), bad, new Uint8Array(m)) };
  }, [fromB, msg]);
  return { onB, onA };
});
check(e4 && e4.onB && e4.onB.ok && !e4.onB.bad && e4.onA.ok && !e4.onA.bad, 'E4: its signature checks natively, a native one checks there, a changed one nowhere', JSON.stringify(e4));

console.log('E5: a native key kept is never replaced');
const ctxD = await browser.newContext();
const D1 = await page(false, ctxD);
const idD = await D1.evaluate(() => ownDeviceId());
await D1.close();
const D2 = await page(true, ctxD);
const idD2 = await err(() => D2.evaluate(() => ownDeviceId()));
const kindD = await kindOf(D2);
check(idD === idD2 && kindD === 'key', 'E5: the same device, its own key kept', JSON.stringify({ idD, idD2, kindD }));

console.log('E6: no Ed25519 and no tweetnacl');
const ctxE = await browser.newContext();
await ctxE.route(/tweetnacl/, (r) => r.abort());
const E = await page(true, ctxE);
const e = await err(() => E.evaluate(() => ownDeviceId()));
check(/Update Safari/.test(e) && !/not supported/i.test(e), 'E6: said plainly: ' + e, e);

const errs = [A, B, E].flatMap((p) => p.errs);
console.log('page errors: ' + (errs.join(' | ') || 'none'));
await browser.close();
console.log(fails ? fails + ' FAILED' : 'all passed');
process.exit(fails ? 1 : 0);
