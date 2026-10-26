// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// The viewer under its content policy, as the relay serves it (run.sh):
//   P1  the page is served with a policy that names its sources (default-src);
//   P2  connected to a session this browser owns, what is typed comes back;
//   P3  Share draws its QR code;
//   P4  an image the machine sends is previewed (an <img> of a blob);
//   P5  a PDF the machine sends is previewed (an <iframe> of a blob);
//   P6  any other file downloads (a link to a blob);
//   P7  the app list shows an app's own icon (a data: image) and, for an
//       icon on another host, the app's letter — that host never asked;
//   P8  alerts' service worker registers, and the page fetches from its own
//       origin (a relay with no push key answers /push/key 404: not asked);
//   P9  the tour's fonts load;
//   P10 none of it refused: no policy violation, no console error, nothing
//       thrown.
import { execSync, spawn } from 'node:child_process';
import { openSync } from 'node:fs';
import { chromium } from 'playwright';

let fails = 0;
const sh = (c) => execSync(c, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function until(fn, secs) { for (const end = Date.now() + secs * 1000; Date.now() < end; await sleep(300)) { try { if (await fn()) return true; } catch (_) {} } return false; }
function result(id, what, ok, why) {
  console.log(`${ok ? 'PASS' : 'FAIL'} ${id}: ${what}${ok ? '' : '  — ' + why}`);
  if (!ok) fails++;
}

spawn('reminal-relay', ['relay', '8080'], { detached: true, stdio: 'ignore' }).unref();
await until(async () => (await fetch('http://127.0.0.1:8080/')).ok, 30);

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1300, height: 900 }, acceptDownloads: true });
await ctx.addInitScript(() => {
  window.__csp = [];
  document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(e.violatedDirective + ' ' + (e.blockedURI || '') + ' ' + (e.sourceFile || '') + ':' + (e.lineNumber || '')));
});
const page = await ctx.newPage();
const errs = [], said = [], refused = [], offHost = [], failed = [];
page.on('response', (r) => { if (r.status() >= 400) failed.push(r.status() + ' ' + r.url()); });
page.on('pageerror', (e) => errs.push(e.message));
page.on('console', (m) => {
  if (/Content Security Policy|Refused to/i.test(m.text())) refused.push(m.text().slice(0, 240));
  else if (m.type() === 'error') said.push(m.text().slice(0, 240));
});
const ICON_HOST = 'icons.example.invalid';
page.on('request', (r) => { if (r.url().includes(ICON_HOST)) offHost.push(r.url()); });
page.on('dialog', (d) => (/has not connected to this machine before/.test(d.message()) ? d.accept() : d.dismiss()));

const resp = await page.goto('http://127.0.0.1:8080/');
const policy = resp.headers()['content-security-policy'] || '';
result('P1', 'the page is served with a policy that names its sources', /(^|;\s*)default-src 'none'/.test(policy) && /connect-src /.test(policy) && /img-src /.test(policy), policy);
await page.evaluate(() => { localStorage.setItem('reminal-tour-v1', 'skipped'); });
await page.reload();
await page.waitForFunction(() => typeof connectOwned === 'function' && window.reminalTour, null, { timeout: 30000 });

sh(`reminal add owner ${await page.evaluate(() => ownDeviceId())} -y`);
const dlog = openSync('/tmp/csp-daemon.log', 'a');
spawn('reminal', ['daemon'], { detached: true, stdio: ['ignore', dlog, dlog] }).unref();
await until(() => { try { sh('reminal list'); return true; } catch (_) { return false; } }, 30);
const work = sh('mktemp -d').trim();
const sid = sh(`cd ${work} && reminal new csp`).match(/Session:\s+([A-Z0-9]+)/)[1];
await page.evaluate((s) => connectOwned(s), sid);
const live = await until(() => page.evaluate(() => typeof agentLive !== 'undefined' && agentLive && !!cryptoKey), 60);
await page.evaluate(() => sendInput('echo CSP-$((6*7))\r'));
const typed = live && await until(() => page.evaluate(() => (document.querySelector('.xterm-rows')?.innerText || '').includes('CSP-42')), 20);
result('P2', 'connected, and what is typed comes back', typed, JSON.stringify({ live }));

await page.evaluate(() => openShare());
const qr = await until(() => page.evaluate(() => [...document.querySelectorAll('#share-qr img, #share-qr canvas')].some((e) => (e.tagName === 'CANVAS' ? e.width : e.naturalWidth) > 0)), 10);
await page.evaluate(() => closeShare());
result('P3', 'Share draws its QR code', qr, 'no QR drawn');

// A 1x1 PNG, as a machine sends a file.
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
await page.evaluate((b) => triggerDownload('dot.png', b), PNG);
const img = await until(() => page.evaluate(() => { const i = document.querySelector('#preview-frame img'); return !!i && i.complete && i.naturalWidth === 1; }), 10);
result('P4', 'an image is previewed', img, 'the preview image did not load');
const PDF = Buffer.from('%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj 2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj 3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 72 72]>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n').toString('base64');
await page.evaluate((b) => triggerDownload('doc.pdf', b), PDF);
const pdf = await until(() => page.evaluate(() => { const f = document.querySelector('#preview-frame iframe'); return !!f && f.src.startsWith('blob:'); }), 10);
await sleep(1500);
await page.evaluate(() => document.getElementById('preview-close').click());
result('P5', 'a PDF is previewed', pdf, 'no preview frame');
const dl = page.waitForEvent('download', { timeout: 10000 }).catch(() => null);
await page.evaluate((b) => triggerDownload('notes.txt', b), Buffer.from('hello\n').toString('base64'));
const got = await dl;
result('P6', 'any other file downloads', !!got && got.suggestedFilename() === 'notes.txt', 'no download');

await page.evaluate(([png, host]) => {
  onAppList({ apps: [{ name: 'Local', app: 'local', icon: 'data:image/png;base64,' + png }, { name: 'Zed', app: 'zed', icon: 'https://' + host + '/zed.png' }] });
}, [PNG, ICON_HOST]);
await sleep(1000);
const apps = await page.evaluate(() => [...document.querySelectorAll('#apps-list .app-list-icon')].map((e) => ({ tag: e.tagName, ok: e.tagName === 'IMG' ? e.complete && e.naturalWidth > 0 : true, text: e.textContent })));
result('P7', "the app list shows an app's own icon, and a letter for an icon on another host, never asked for",
  apps.length === 2 && apps[0].tag === 'IMG' && apps[0].ok && apps[1].tag === 'SPAN' && apps[1].text === 'Z' && !offHost.length,
  JSON.stringify({ apps, asked: offHost }));

const sw = await page.evaluate(async () => {
  const reg = await pushRegistration();
  const r = await fetch('/manifest.webmanifest', { cache: 'no-store' });
  return { active: !!(reg && reg.active), fetched: r.ok };
}).catch((e) => ({ err: String(e) }));
result('P8', "alerts' service worker registers, and the page fetches from its own origin", sw.active && sw.fetched, JSON.stringify(sw));

await page.evaluate(() => window.reminalTour.start());
await until(() => page.evaluate(() => !!document.querySelector('.tour-card')), 20);
const fonts = await until(() => page.evaluate(async () => { await document.fonts.ready; return document.fonts.check('16px "Tour Geist"') && [...document.fonts].some((f) => f.family.includes('Tour Geist') && f.status === 'loaded'); }), 20);
await page.evaluate(() => { for (const e of document.querySelectorAll('.tour-card, .tour-block, .tour-ring, .tour-mock')) e.remove(); });
result('P9', "the tour's fonts load", fonts, JSON.stringify(await page.evaluate(() => [...document.fonts].map((f) => f.family + ':' + f.status))));

const violations = await page.evaluate(() => window.__csp);
result('P10', 'nothing refused by the policy, no console error, nothing thrown',
  !violations.length && !refused.length && !said.length && !errs.length,
  JSON.stringify({ violations: violations.slice(0, 8), refused: refused.slice(0, 8), console: said.slice(0, 5), thrown: errs.slice(0, 5), failed: failed.slice(0, 5) }));

await browser.close();
console.log(fails ? `${fails} failed` : 'all passed');
process.exit(fails ? 1 : 0);
