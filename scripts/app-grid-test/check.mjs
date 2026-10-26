// The Apps launcher lays its apps out as tiles three across, icon over name,
// under the search box that still filters them, and a tile still launches its
// app; the panel fits the screen on a phone, a tablet and a desktop (run.sh).
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync } from 'node:fs';
const html = readFileSync('/t/index.html');
const srv = http.createServer((q, r) => { const a = /^\/assets\/([\w.-]+\.js)$/.exec(q.url.split('?')[0]); if (a) { let b; try { b = readFileSync('/t/assets/' + a[1]); } catch (_) { r.writeHead(404); return r.end(); } r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(b); } if (q.url.split('?')[0] === '/reload.js') { r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(readFileSync('/t/reload.js')); } r.writeHead(200, { 'content-type': 'text/html' }); r.end(html); }).listen(8099);
const APPS = ['Activity Monitor', 'AdBlock', 'AirPort Utility', 'Android Studio', 'AnyDesk', 'App Store',
  'Audio MIDI Setup', 'Calculator', 'Calendar', 'Microsoft Remote Desktop Connection Beta', 'Safari', 'Terminal']
  .map((name, i) => ({ id: 'a' + i, name }));
const SIZES = [
  ['phone', { viewport: { width: 412, height: 900 }, isMobile: true, hasTouch: true }],
  ['tablet', { viewport: { width: 900, height: 1000 }, isMobile: true, hasTouch: true }],
  ['desktop', { viewport: { width: 1280, height: 800 } }],
];
const b = await chromium.launch();
const fails = [];
for (const [name, opts] of SIZES) {
  const ctx = await b.newContext(opts);
  const page = await ctx.newPage();
  await page.goto('http://127.0.0.1:8099/');
  await page.evaluate((apps) => {
    window.__sent = [];
    // No host here: these stand in for its answers.
    sendWin = (type, obj) => {
      window.__sent.push(type);
      if (type === 'window_list') setTimeout(() => onWindowList({ windows: [] }), 10);
    };
    document.body.classList.add('connected');
    document.getElementById('apps').open = true;
    onAppList({ apps });
  }, APPS);
  await page.waitForTimeout(300);
  if (process.env.SHOTS) await page.screenshot({ path: process.env.SHOTS + '/apps-' + name + '.png' });
  const got = await page.evaluate(() => {
    const root = document.getElementById('apps-list');
    const panel = root.getBoundingClientRect();
    const search = root.querySelector('input.panel-search');
    const tiles = [...root.querySelectorAll('.app-tile')];
    const tops = tiles.map(t => Math.round(t.getBoundingClientRect().top));
    const firstRow = tops.filter(t => t === tops[0]).length;
    const t0 = tiles[0] && tiles[0].getBoundingClientRect();
    const ic = tiles[0] && tiles[0].querySelector('.app-list-icon').getBoundingClientRect();
    const nm = tiles[0] && tiles[0].querySelector('.name').getBoundingClientRect();
    return {
      vw: innerWidth, left: panel.left, right: panel.right, width: Math.round(panel.width),
      search: !!search && search.offsetParent !== null, tiles: tiles.length, firstRow,
      iconAboveName: !!(ic && nm && ic.bottom <= nm.top + 1), tileW: t0 ? Math.round(t0.width) : 0,
    };
  });
  let filtered = [];
  if (got.search && got.tiles) {
    await page.fill('#apps-list input.panel-search', 'saf');
    filtered = await page.$$eval('#apps-list .app-tile .name', ns => ns.map(n => n.textContent));
    if (filtered.length) await page.click('#apps-list .app-tile');
    await page.waitForTimeout(300);
  }
  const sent = await page.evaluate(() => window.__sent);
  console.log(name, JSON.stringify({ ...got, filtered, sent }));
  const bad = [];
  if (!got.search) bad.push('no search box');
  if (got.tiles !== APPS.length) bad.push(got.tiles + ' tiles, not ' + APPS.length);
  if (got.firstRow !== 3) bad.push(got.firstRow + ' across, not 3');
  if (!got.iconAboveName) bad.push('icon not above the name');
  if (got.left < 0 || got.right > got.vw) bad.push('panel runs off the screen');
  if (got.width > 400) bad.push('panel ' + got.width + 'px wide, stretched');
  if (filtered.length !== 1 || filtered[0] !== 'Safari') bad.push('search "saf" left ' + JSON.stringify(filtered));
  if (!sent.includes('app_open')) bad.push('tapping a tile did not launch it');
  if (bad.length) fails.push(name + ': ' + bad.join(', '));
  await ctx.close();
}
console.log(fails.length ? 'FAIL  ' + fails.join('; ') : 'PASS  apps are tiles three across under a working search, and a tile launches, on phone, tablet and desktop');
await b.close(); srv.close();
process.exit(fails.length ? 1 : 0);
