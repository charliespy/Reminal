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
  const shown = [], kept = [], posted = [], opened = [];
  const handlers = {};
  const self = {
    addEventListener: (t, f) => { handlers[t] = f; },
    skipWaiting: () => {},
    registration: { showNotification: async (title, o) => { shown.push({ title, ...o }); }, getNotifications: async () => [] },
    clients: { matchAll: async () => [{ postMessage: (m) => posted.push(m) }], openWindow: async (u) => { opened.push(u); }, claim: async () => {} },
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
  // Where a tapped notification may take the page: home, or one session by
  // its id; never a PIN (it rides in a fragment), an invite, or anywhere else.
  const urlOf = async (url) => { await push({ title: 'box', body: 'x', tag: 'u' + shown.length, url }); return shown[shown.length - 1].data.url; };
  const kept2 = [await urlOf('/'), await urlOf('/?s=ABCD2345')];
  check(kept2[0] === '/' && kept2[1] === '/?s=ABCD2345', 'home, and one session by its id, are kept', JSON.stringify(kept2));
  const crafted = ['/?s=ABCD2345#p=12345678', '/?s=ABCD2345&invite=x', '/?s=abcd2345', '/?s=ABCD234I', '/?s=ABCD23456', '/join', '//evil.example/', 'https://evil.example/', '/?s=ABCD2345 ', 42];
  const went = [];
  for (const u of crafted) went.push(await urlOf(u));
  check(went.every((u) => u === '/'), 'a crafted url (a PIN fragment, another parameter, a bad id, another path or host) is home', JSON.stringify(went));
  const tap = async (data) => { let p; handlers.notificationclick({ notification: { data, close: () => {} }, waitUntil: (x) => { p = x; } }); await p; return opened[opened.length - 1]; };
  const tapped = [await tap({ url: '/?s=ABCD2345#p=12345678' }), await tap({ url: '/?s=ABCD2345' }), await tap(undefined)];
  check(tapped[0] === '/' && tapped[1] === '/?s=ABCD2345' && tapped[2] === '/', 'tapped, a notification shown before this check opens home for a crafted url, its session for a good one', JSON.stringify(tapped));
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

// Turned on before tokens: as soon as such a machine answers (the Machines
// panel), the page says so, sheet closed, with one tap to renew.
await page.evaluate(async ([KEY, OLD]) => { const st = await loadOwnedMachines(); await checkAlertRenewFor(KEY, st[KEY]); await checkAlertRenewFor(OLD, st[OLD]); }, [KEY, OLD]);
const bar = await page.evaluate(() => { const el = document.getElementById('alerts-renew'); return el ? { hidden: el.hidden, text: el.textContent } : null; });
check(bar && !bar.hidden && /from box need renewing on this device/.test(bar.text) && /Renew alerts on this phone/.test(bar.text) && !/oldbox/.test(bar.text),
  'the Machines panel asks to renew, sheet closed, for the machine that takes a token', JSON.stringify(bar));
await page.evaluate(() => document.querySelector('#alerts-renew button').click());
for (let i = 0; i < 40 && !asked.length; i++) await new Promise(r => setTimeout(r, 100));
check(asked.length === 1 && asked[0].machine === std(KEY), 'its one tap asks the token for that machine', JSON.stringify(asked));
asked.length = 0;

// The sheet offers it too — only for the machine that takes a token.
await page.evaluate(() => openAlerts());
await page.waitForFunction(() => !!document.querySelector('#al-renew'), null, { timeout: 15000 }).catch(() => {});
const renewText = await page.evaluate(() => (document.querySelector('.al-renew') || {}).textContent || '');
check(/box/.test(renewText) && !/oldbox/.test(renewText) && /Renew alerts on this phone/.test(renewText) && /no alerts from it/.test(renewText),
  'Renew alerts on this phone, for the machine that takes a token, saying it gets none until then', JSON.stringify(renewText));
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
check(!(await page.evaluate(() => !!document.querySelector('#al-renew'))) && (await page.evaluate(() => document.getElementById('alerts-renew').hidden)), 'Renew goes (sheet and panel) once the machine has it', 'still offered');

// A token its machine refuses (alerts off there) is dropped, not handed on
// again each time; one that did not reach the machine is kept for later.
const kept = await page.evaluate(async ([m, t]) => {
  const c = await caches.open('reminal-alert-tokens');
  const qd = queryDirectory;
  queryDirectory = async () => ({ push: { error: 'this phone has no alerts on this machine' } });
  await c.put('/alert-token/' + encodeURIComponent(m), new Response(t)); await deliverAlertTokens();
  const refused = (await c.keys()).length;
  queryDirectory = async () => null;
  await c.put('/alert-token/' + encodeURIComponent(m), new Response(t)); await deliverAlertTokens();
  const unreached = (await c.keys()).length;
  await c.delete('/alert-token/' + encodeURIComponent(m));
  queryDirectory = qd;
  return { refused, unreached };
}, [std(KEY), TOKEN]);
check(kept.refused === 0 && kept.unreached === 1, 'a token its machine refuses is dropped, one that did not reach it kept', JSON.stringify(kept));

// A machine turned on now: bound from the start, no Renew needed.
const before = asked.length;
await page.evaluate(async (NEW) => { await withOwnedMachines(m => { m[NEW] = { name: 'newbox', lastSeen: Date.now() }; }); await syncAllAlerts(); }, NEW);
await page.waitForFunction((n) => true, before);
for (let i = 0; i < 40 && asked.length === before; i++) await new Promise(r => setTimeout(r, 100));
check(asked.length === before + 1 && asked[before].machine === std(NEW), 'alerts turned on for a machine ask its token at once', JSON.stringify(asked.slice(before)));
check(!asked.some(a => a.machine === std(OLD)), 'a machine too old for tokens is never asked one', JSON.stringify(asked));
// What the page really sends a machine for a token (the calls above stand
// in for the machine): the token in the request, and in what the owner signs.
const req = await page.evaluate(() => {
  const sub = { endpoint: 'https://fcm.googleapis.com/fcm/send/phone', p256dh: 'p', auth: 'a' }, tok = 'v1.1797000000.' + 'z'.repeat(43);
  return { token: pushRequestOf({ op: 'token', sub, token: tok }, { x: 1 }, 'r1'), set: pushRequestOf({ op: 'set', sub, rules: { cpu: true } }, { x: 1 }, 'r2'),
    act: pushActionOf({ op: 'token', sub, token: tok }), actSet: pushActionOf({ op: 'set', sub }) };
});
check(req.token.push.token === 'v1.1797000000.' + 'z'.repeat(43) && req.token.push.op === 'token' && req.token.push.req_id === 'r1' && req.set.push.token === undefined,
  'the request for a token carries the token (and only that one does)', JSON.stringify(req));
check(req.act === 'push:token:https://fcm.googleapis.com/fcm/send/phone:v1.1797000000.' + 'z'.repeat(43) && req.actSet === 'push:set:https://fcm.googleapis.com/fcm/send/phone',
  'the owner signs the token with it (pushAction on the machine)', JSON.stringify([req.act, req.actSet]));
check(!errs.length, 'no page errors', errs.join(' | '));
await browser.close();
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
