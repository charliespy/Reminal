// run.sh: alerts bound to the machine they are turned on for — the page's
// side and the service worker's.
import { chromium } from 'playwright';
import http from 'node:http';
import vm from 'node:vm';
import { readFileSync } from 'node:fs';

let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const check = (c, good, why) => (c ? ok(good) : fail(good + ' — ' + why));

// ---- the service worker, with what it is given by the browser stood in -----
{
  const shown = [], kept = [], posted = [];
  const handlers = {};
  const self = {
    addEventListener: (t, f) => { handlers[t] = f; },
    skipWaiting: () => {},
    registration: { showNotification: async (title, o) => { shown.push({ title, ...o }); }, getNotifications: async () => [] },
    clients: { matchAll: async () => [{ postMessage: (m) => posted.push(m) }], claim: async () => {} },
  };
  const caches = { open: async (name) => ({ put: async (req, res) => { kept.push([name, req, await res.text()]); } }) };
  vm.runInNewContext(readFileSync('/t/sw.js', 'utf8'), { self, caches, Response, URL, Date, String, Number, encodeURIComponent, JSON });
  const push = async (d) => { let p; handlers.push({ data: { json: () => d, text: () => JSON.stringify(d) }, waitUntil: (x) => { p = x; } }); await p; };
  const machine = 'A'.repeat(42) + 'E=', token = 'v1.1797000000.' + 'b'.repeat(43);
  await push({ kind: 'alerts-on', machine, token, at: 1 });
  check(shown.length === 1 && shown[0].title === 'reminal' && shown[0].body === 'Alerts are on for this device.' && shown[0].tag === 'alerts-on:' + machine,
    'the relay’s Alerts are on is shown, in its own words, one per machine', JSON.stringify(shown));
  check(kept.length === 1 && kept[0][0] === 'reminal-alert-tokens' && kept[0][1] === '/alert-token/' + encodeURIComponent(machine) && kept[0][2] === token,
    'its token is kept for the page, under its machine', JSON.stringify(kept));
  check(posted.length === 1 && posted[0].type === 'alert-token', 'an open page is told', JSON.stringify(posted));
  await push({ kind: 'alerts-on', machine: 'nope', token: 'v1.1.x' });
  check(shown.length === 2 && kept.length === 1, 'a malformed one is shown (a push must show) but nothing is kept', JSON.stringify(kept));
  await push({ title: 'box', body: 'Battery at 12%', tag: 'battery' });
  check(shown.length === 3 && shown[2].title === 'box' && shown[2].body === 'Battery at 12%', 'an alert shows as before', JSON.stringify(shown[2]));
}

// ---- the page ---------------------------------------------------------------
const html = readFileSync('/t/index.html');
const asked = [];
http.createServer((q, r) => {
  const p = q.url.split('?')[0];
  if (p === '/push/token') {
    let b = ''; q.on('data', (c) => { b += c; }); q.on('end', () => { asked.push(JSON.parse(b)); r.writeHead(200, { 'content-type': 'application/json' }); r.end('{"status":201}'); });
    return;
  }
  let f = null;
  for (const dir of ['/t']) { try { f = readFileSync(dir + (p === '/' ? '/index.html' : p)); break; } catch (_) {} }
  if (!f) { r.writeHead(404); return r.end(); }
  r.writeHead(200, { 'content-type': p.endsWith('.js') ? 'text/javascript' : p.endsWith('.css') ? 'text/css' : 'text/html' });
  r.end(f);
}).listen(8099);

const browser = await chromium.launch();
const ctx = await browser.newContext();
await ctx.grantPermissions(['notifications'], { origin: 'http://127.0.0.1:8099' });
const page = await ctx.newPage();
const errs = [];
page.on('pageerror', (e) => errs.push(e.message));
await page.goto('http://127.0.0.1:8099/');
await page.waitForFunction(() => typeof openAlerts === 'function' && typeof withOwnedMachines === 'function', null, { timeout: 30000 });

// A phone subscription and machines, stood in: each machine answers a push
// request as a machine of that age does.
const KEY = 'QUJD' + 'A'.repeat(39), OLD = 'T0xE' + 'A'.repeat(39), NEW = 'TkVX' + 'A'.repeat(39);
const std = (k) => { let t = k.replace(/-/g, '+').replace(/_/g, '/'); while (t.length % 4) t += '='; return t; };
await page.evaluate(([KEY, OLD]) => {
  window.__ops = [];
  window.__tokened = {};
  const SUB = { endpoint: 'https://fcm.googleapis.com/fcm/send/phone', p256dh: 'B'.repeat(87), auth: 'C'.repeat(22) };
  pushSubscription = async () => SUB;
  queryDirectory = async (pub, o) => {
    const k = btoa(String.fromCharCode(...pub)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    const q = o.push;
    __ops.push({ k, op: q.op, token: q.token || '' });
    if (q.op === 'token') __tokened[k] = q.token;
    const push = { subscribed: q.op !== 'remove', rules: q.rules || {} };
    if (k !== OLD) { push.tokens = true; push.tokened = !!__tokened[k]; }
    return { push };
  };
}, [KEY, OLD]);
await page.evaluate(async ([KEY, OLD]) => {
  const prefs = { enabled: true, rules: normAlertRules({}) };
  await saveAlertPrefs(prefs);
  await withOwnedMachines(m => {
    for (const k of [KEY, OLD]) m[k] = { name: k === KEY ? 'box' : 'oldbox', lastSeen: Date.now() };
  });
  // Both already told (alerts on before tokens): nothing to sync.
  await withOwnedMachines(m => { for (const k of [KEY, OLD]) m[k].alertsSynced = alertWantKey(prefs, m[k]); });
}, [KEY, OLD]);

// Turned on before tokens: the sheet offers to renew it — only for the
// machine that takes a token.
await page.evaluate(() => openAlerts());
await page.waitForFunction(() => !!document.querySelector('#al-renew'), null, { timeout: 15000 }).catch(() => {});
const renewText = await page.evaluate(() => (document.querySelector('.al-renew') || {}).textContent || '');
check(/box/.test(renewText) && !/oldbox/.test(renewText) && /Renew alerts on this phone/.test(renewText) && /15 Dec 2026|Dec 15, 2026/.test(renewText),
  'Renew alerts on this phone, for the machine that takes a token, with the date they stop', JSON.stringify(renewText));
await page.click('#al-renew');
await page.waitForFunction(() => /Renewing/.test((document.querySelector('.al-msg') || {}).textContent || ''), null, { timeout: 10000 }).catch(() => {});
check(asked.length === 1 && asked[0].machine === std(KEY) && asked[0].endpoint === 'https://fcm.googleapis.com/fcm/send/phone' && asked[0].p256dh && asked[0].auth,
  'Renew asks the relay for a token for this phone and that machine', JSON.stringify(asked));

// The token, as the service worker keeps it: handed to its machine, once.
const TOKEN = 'v1.1797000000.' + 'z'.repeat(43);
await page.evaluate(async ([m, t]) => { await (await caches.open('reminal-alert-tokens')).put('/alert-token/' + encodeURIComponent(m), new Response(t)); await deliverAlertTokens(); }, [std(KEY), TOKEN]);
const ops = await page.evaluate(() => __ops.filter(o => o.op === 'token'));
check(ops.length === 1 && ops[0].k === KEY && ops[0].token === TOKEN, 'the kept token is handed to its machine', JSON.stringify(ops));
check((await page.evaluate(async () => (await (await caches.open('reminal-alert-tokens')).keys()).length)) === 0, 'and no longer kept', 'still kept');
await page.waitForFunction(() => !document.querySelector('#al-renew'), null, { timeout: 10000 }).catch(() => {});
check(!(await page.evaluate(() => !!document.querySelector('#al-renew'))), 'Renew goes once the machine has it', 'still offered');

// A machine turned on now: bound from the start, no Renew needed.
const before = asked.length;
await page.evaluate(async (NEW) => { await withOwnedMachines(m => { m[NEW] = { name: 'newbox', lastSeen: Date.now() }; }); await syncAllAlerts(); }, NEW);
await page.waitForFunction((n) => true, before);
for (let i = 0; i < 40 && asked.length === before; i++) await new Promise(r => setTimeout(r, 100));
check(asked.length === before + 1 && asked[before].machine === std(NEW), 'alerts turned on for a machine ask its token at once', JSON.stringify(asked.slice(before)));
check(!asked.some(a => a.machine === std(OLD)), 'a machine too old for tokens is never asked one', JSON.stringify(asked));
check(!errs.length, 'no page errors', errs.join(' | '));
await browser.close();
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
