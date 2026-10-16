// e2e.sh: phone alerts from the page to a real machine and back.
import { chromium } from 'playwright';
import { execSync, spawn } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { webcrypto } from 'node:crypto';

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
const pushFile = () => { try { return JSON.parse(readFileSync('/root/.reminal/push.json', 'utf8')); } catch (_) { return null; } };

// The relay and the machine, this tree's.
spawn('reminal', ['relay', '8080'], { detached: true, stdio: 'ignore' }).unref();
if (!(await until(async () => (await fetch('http://127.0.0.1:8080/')).ok, 20))) { fail('the relay did not start'); process.exit(1); }

// A phone's subscription, as a push service would give it (a headless
// browser has none): real keys, so the machine takes it.
const b64u = (u8) => Buffer.from(u8).toString('base64url');
const ua = await webcrypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveBits']);
const SUB = { endpoint: 'https://fcm.googleapis.com/fcm/send/e2e-phone', p256dh: b64u(new Uint8Array(await webcrypto.subtle.exportKey('raw', ua.publicKey))), auth: b64u(webcrypto.getRandomValues(new Uint8Array(16))) };
const TOKEN = 'v1.1797000000.' + 'q'.repeat(43);

const browser = await chromium.launch();
const ctx = await browser.newContext();
await ctx.grantPermissions(['notifications'], { origin: 'http://127.0.0.1:8080' });
await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
const asked = [];
await ctx.route('**/push/token', async (r) => { asked.push(JSON.parse(r.request().postData() || '{}')); await r.fulfill({ json: { status: 201 } }); });
const page = await ctx.newPage();
const errs = [];
page.on('pageerror', e => errs.push(e.message));
page.on('dialog', d => d.accept());
const standIn = (p) => p.evaluate((SUB) => { pushSubscription = async () => SUB; }, SUB);
await page.goto('http://127.0.0.1:8080/');
await page.waitForFunction(() => typeof ownDeviceId === 'function' && typeof syncAllAlerts === 'function');
await standIn(page);

// This browser owns the machine.
sh(`reminal add owner ${await page.evaluate(() => ownDeviceId())} -y`);
spawn('reminal', ['daemon'], { detached: true, stdio: 'ignore' }).unref();
await sleep(1500);
const sid = sh('reminal new first').match(/Session:\s+([A-Z0-9]+)/)[1];
await page.evaluate(s => connectOwned(s), sid);
check(await until(() => page.evaluate(async () => Object.keys(await loadOwnedMachines()).length === 1), 40), 'the browser owns the machine', 'no machine');
await page.evaluate(() => userDisconnect());
const KEY = await page.evaluate(async () => Object.keys(await loadOwnedMachines())[0]);
const STD = (() => { let t = KEY.replace(/-/g, '+').replace(/_/g, '/'); while (t.length % 4) t += '='; return t; })();

// Alerts turned on from the page: the machine keeps this phone; it has no
// token for it yet, and the page asks the relay for one at once.
await page.evaluate(async () => { await saveAlertPrefs({ enabled: true, rules: normAlertRules({}) }); alerts.sub = await pushSubscription(false); return syncAllAlerts(); });
const f1 = pushFile();
check(f1 && f1.subs.length === 1 && f1.subs[0].sub.endpoint === SUB.endpoint && !f1.subs[0].token && !f1.subs[0].new_token,
  'turned on from the page, the machine keeps this phone (no token yet)', JSON.stringify(f1));
check(await until(() => asked.length === 1, 10) && asked[0].machine === STD && asked[0].endpoint === SUB.endpoint, 'and the page asks a token for it, for that machine', JSON.stringify(asked));

// The page opened again (as after the relay began to bind alerts): the
// Machines panel finds the machine has this phone without a token, and asks.
await page.reload();
await page.waitForFunction(() => typeof renderMachines === 'function');
await standIn(page);
await page.evaluate(() => renderMachines());
const shown = await until(() => page.evaluate(() => { const el = document.getElementById('alerts-renew'); return el && !el.hidden; }), 30);
check(shown, 'opened again, the Machines panel asks to renew alerts on this phone', 'no prompt: ' + await page.evaluate(() => (document.getElementById('alerts-renew') || {}).outerHTML));
const before = asked.length;
await page.evaluate(() => document.querySelector('#alerts-renew button').click());
check(await until(() => asked.length === before + 1, 10), 'one tap asks the token again', JSON.stringify(asked.slice(before)));

// The token, as the service worker keeps it from the relay's notification:
// the page hands it to the machine — the real request, the owner's proof.
await page.evaluate(async ([m, t]) => { await (await caches.open('reminal-alert-tokens')).put('/alert-token/' + encodeURIComponent(m), new Response(t)); await deliverAlertTokens(); }, [STD, TOKEN]);
const f2 = pushFile();
check(f2 && f2.subs[0].new_token === TOKEN, 'the machine keeps the token the page handed it', JSON.stringify(f2 && f2.subs[0]));
check(await until(() => page.evaluate(() => document.getElementById('alerts-renew').hidden), 10), 'and the prompt goes', 'still shown');
// A token the owner did not sign for is refused by the machine.
const forged = await page.evaluate(async ([KEY, SUB]) => {
  try { await queryDirectory(base64ToU8(b64URLToStd(KEY)), { push: { op: 'token', sub: SUB, token: 'v1.1797000000.' + 'x'.repeat(43) }, timeout: 10000 }).then(r => { if (r.push && r.push.error) throw new Error(r.push.error); }); return 'taken'; }
  catch (e) { return 'refused: ' + e.message; }
}, [KEY, SUB]);
check(/^taken$/.test(forged) && pushFile().subs[0].new_token === 'v1.1797000000.' + 'x'.repeat(43), 'the page’s own request, its proof naming the token, is taken by the machine', forged);
check(!errs.length, 'no page errors', errs.join(' | '));
await browser.close();
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
