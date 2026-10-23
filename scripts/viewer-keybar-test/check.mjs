// run.sh: the command bar's ^X, ^S and ^Q, after ^O. Each sends exactly its
// byte (0x18, 0x13, 0x11) tapped on a phone and clicked on a desktop, and
// its Ctrl key to a remote window when the window keyboard is up; each looks
// like ^O; and the bar at phone width scrolls rather than wraps.
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync, existsSync } from 'node:fs';
const html = readFileSync('/t/index.html');
http.createServer((q, r) => { const a = /^\/assets\/([\w.-]+\.js)$/.exec(q.url.split('?')[0]); if (a) { let b; try { b = readFileSync('/t/assets/' + a[1]); } catch (_) { r.writeHead(404); return r.end(); } r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(b); } if (q.url.split('?')[0] === '/reload.js') { r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(readFileSync('/t/reload.js')); } r.writeHead(200, { 'content-type': 'text/html' }); r.end(html); }).listen(8099);
const b = await chromium.launch();
let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const WANT = [['ctrl-x', '\x18'], ['ctrl-s', '\x13'], ['ctrl-q', '\x11']];
const hex = (s) => [...s].map((c) => '0x' + c.charCodeAt(0).toString(16).padStart(2, '0')).join(' ');
const shots = existsSync('/out');

async function open(name, opts) {
  const ctx = await b.newContext(opts);
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', '1'); } catch (_) {} });
  const page = await ctx.newPage();
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForFunction(() => typeof term === 'object' && term && document.querySelector('#keybar'));
  await page.evaluate(() => {
    window.__got = []; window.__win = [];
    inputLive = () => true;
    deliverInput = (d) => { window.__got.push(d); };
  });
  // The bar's shape: how wide it is against what it shows, and how it lays out.
  const bar = await page.evaluate(() => {
    const k = document.getElementById('keybar'); const cs = getComputedStyle(k);
    const rows = new Set([...k.querySelectorAll('button')].filter((x) => x.offsetParent).map((x) => x.offsetTop));
    return { scrollW: k.scrollWidth, clientW: k.clientWidth, overflowX: cs.overflowX, wrap: cs.flexWrap, rows: rows.size };
  });
  console.log(`info ${name}: bar ${bar.scrollW}px of content in ${bar.clientW}px (overflow-x ${bar.overflowX}, flex-wrap ${bar.wrap}, ${bar.rows} row(s))`);
  if (shots) await page.locator('#keybar').screenshot({ path: `/out/keybar-${name}.png` });
  return { ctx, page, bar };
}

// A desktop with only a mouse has no bar at all, by design (index.html's
// pointer media query): it must stay hidden there.
{
  const ctx = await b.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await ctx.newPage();
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForFunction(() => document.querySelector('#keybar'));
  const d = await page.evaluate(() => getComputedStyle(document.getElementById('keybar')).display);
  d === 'none' ? ok('desktop-mouse-1280: no bar, as before (a mouse-only desktop has none)') : fail('desktop-mouse-1280: the bar shows (' + d + ')');
  await ctx.close();
}

for (const [name, opts, how] of [
  ['phone-390', { viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 3 }, 'tap'],
  ['desktop-touch-1280', { viewport: { width: 1280, height: 800 }, hasTouch: true }, 'click'],
]) {
  const { ctx, page, bar } = await open(name, opts);
  const present = await page.evaluate(() => [...document.querySelectorAll('#keybar button[data-key]')].map((x) => x.dataset.key));
  const at = present.indexOf('ctrl-o');
  if (at < 0 || present.slice(at + 1, at + 4).join(',') !== 'ctrl-x,ctrl-s,ctrl-q') { fail(`${name}: after ^O the bar has ${present.slice(at, at + 5).join(', ')}`); }
  else ok(`${name}: ^X, ^S, ^Q follow ^O`);
  // Each looks like ^O: the same box and the same type.
  const looks = await page.evaluate(() => {
    const st = (k) => { const e = document.querySelector(`#keybar button[data-key="${k}"]`); if (!e) return null; const c = getComputedStyle(e); return { h: e.offsetHeight, w: e.offsetWidth, font: c.font, pad: c.padding, margin: c.margin, border: c.border, bg: c.backgroundColor, color: c.color, radius: c.borderRadius }; };
    return { o: st('ctrl-o'), x: st('ctrl-x'), s: st('ctrl-s'), q: st('ctrl-q') };
  });
  for (const k of ['x', 's', 'q']) {
    const a = looks.o, z = looks[k];
    if (!z) { fail(`${name}: no ^${k.toUpperCase()} button`); continue; }
    const same = ['h', 'font', 'pad', 'margin', 'border', 'bg', 'color', 'radius'].every((f) => a[f] === z[f]) && Math.abs(a.w - z.w) <= 2;
    same ? ok(`${name}: ^${k.toUpperCase()} looks like ^O (${z.w}×${z.h} vs ${a.w}×${a.h})`) : fail(`${name}: ^${k.toUpperCase()} differs from ^O: ${JSON.stringify({ o: a, [k]: z })}`);
  }
  for (const [key, byte] of WANT) {
    if (!present.includes(key)) { fail(`${name}: no ${key} button to ${how}`); continue; }
    await page.evaluate(() => { window.__got = []; });
    const btn = page.locator(`#keybar button[data-key="${key}"]`);
    await btn.scrollIntoViewIfNeeded();
    if (how === 'tap') await btn.tap(); else await btn.click();
    await page.waitForTimeout(150);
    const got = (await page.evaluate(() => window.__got)).join('');
    got === byte ? ok(`${name}: ${how} ${key} sends exactly ${hex(byte)}`) : fail(`${name}: ${how} ${key} sent ${JSON.stringify(hex(got))}, want ${hex(byte)}`);
  }
  // Window keyboard up: the same buttons press Ctrl+key in the remote window.
  const win = await page.evaluate(() => {
    const sent = []; sendWin = (t, m) => { sent.push(m); return Promise.resolve(); };
    focusedPaneId = 'w1'; document.body.classList.add('wkbd-open');
    for (const k of ['ctrl-x', 'ctrl-s', 'ctrl-q']) { const e = document.querySelector(`#keybar button[data-key="${k}"]`); if (e) e.click(); }
    document.body.classList.remove('wkbd-open'); focusedPaneId = null;
    return sent.map((m) => m.kind + ':' + m.special);
  });
  win.join(',') === 'key:ctrl-x,key:ctrl-s,key:ctrl-q' ? ok(`${name}: with the window keyboard up they press Ctrl-X, Ctrl-S, Ctrl-Q in the window`) : fail(`${name}: window keyboard sent ${JSON.stringify(win)}`);
  if (bar.rows !== 1) fail(`${name}: the bar wraps onto ${bar.rows} rows`); else ok(`${name}: the bar is one row`);
  await ctx.close();
}
await b.close();
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
