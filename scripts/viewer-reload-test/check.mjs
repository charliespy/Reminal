// run.sh: a page whose own script is gone reloads once, never in a loop.
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync } from 'node:fs';
const html = readFileSync('/t/index.html');
const app = /\/assets\/(app\.[0-9a-f]{16}\.js)/.exec(String(html))[1];
const header = /\/assets\/(header\.[0-9a-f]{16}\.js)/.exec(String(html))[1];
// which: the script answered 404 while gone > 0.
let which = app;
// gone: how many more times the app's script is answered 404 (Infinity: always).
let gone = 0;
http.createServer((q, r) => {
  const p = q.url.split('?')[0];
  if (p === '/reload.js') { let g; try { g = readFileSync('/t/reload.js'); } catch (_) { r.writeHead(404); return r.end(); } r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(g); }
  const a = /^\/assets\/([\w.-]+\.js)$/.exec(p);
  if (a) {
    if (a[1] === which && gone > 0) { gone--; r.writeHead(404); return r.end(); }
    let b; try { b = readFileSync('/t/assets/' + a[1]); } catch (_) { r.writeHead(404); return r.end(); }
    r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(b);
  }
  r.writeHead(200, { 'content-type': 'text/html' }); r.end(html);
}).listen(8099);
const b = await chromium.launch();
let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const up = (page) => page.evaluate(() => typeof renderNotes === 'function');
// loads counts the page's loads as the browser commits them (its main frame
// navigated): what a person sees as the page reloading.
function loads(page) {
  const n = { v: 0 };
  page.on('framenavigated', (f) => { if (f === page.mainFrame()) n.v++; });
  return n;
}

// The build it names gone once (a relay upgraded under it): reloaded once,
// and up on the build there now.
{
  const ctx = await b.newContext();
  const page = await ctx.newPage();
  gone = 1;
  const pages = loads(page);
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForFunction(() => typeof renderNotes === 'function', null, { timeout: 30000 }).catch(() => {});
  (await up(page)) && pages.v === 2 ? ok('a page whose script was gone reloaded once and came up (' + pages.v + ' loads)') : fail('gone once: up ' + (await up(page)) + ', ' + pages.v + ' loads');
  await ctx.close();
}

// The first of its scripts gone (the guard must not be what it guards).
{
  const ctx = await b.newContext();
  const page = await ctx.newPage();
  gone = 1; which = header;
  const pages = loads(page);
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForFunction(() => typeof renderNotes === 'function', null, { timeout: 30000 }).catch(() => {});
  (await up(page)) && pages.v === 2 ? ok('a page whose header.js was gone reloaded once and came up (' + pages.v + ' loads)') : fail('header gone once: up ' + (await up(page)) + ', ' + pages.v + ' loads');
  which = app;
  await ctx.close();
}

// Its script always gone: one reload, then the page stays as it is.
{
  const ctx = await b.newContext();
  const page = await ctx.newPage();
  gone = Infinity;
  const pages = loads(page);
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForTimeout(8000); // abort cap: long enough for many reloads, were there a loop
  pages.v === 2 && !(await up(page)) ? ok('a script that keeps failing: one reload, then no more (' + pages.v + ' loads)') : fail('always gone: ' + pages.v + ' loads');
  await ctx.close();
}

// Nothing gone: no reload at all.
{
  const ctx = await b.newContext();
  const page = await ctx.newPage();
  gone = 0;
  const pages = loads(page);
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForFunction(() => typeof renderNotes === 'function', null, { timeout: 30000 }).catch(() => {});
  await page.waitForTimeout(1000);
  (await up(page)) && pages.v === 1 ? ok('a page whose scripts load is never reloaded') : fail('nothing gone: up ' + (await up(page)) + ', ' + pages.v + ' loads');
  await ctx.close();
}
await b.close();
console.log(fails ? 'FAIL' : 'PASS');
process.exit(fails ? 1 : 0);
