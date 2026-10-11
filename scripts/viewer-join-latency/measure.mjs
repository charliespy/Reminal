// How soon a viewer opening a long session can work in it: from the page
// opening to (a) the live screen painted (the prompt, READY$, on screen),
// (b) a keystroke typed then echoed back, (c) the whole history in the
// terminal (its line count no longer growing); and the main thread's long
// tasks meanwhile. CPU throttled by THROTTLE (a phone is ~4-6x a laptop).
// PLAIN=1 hides DecompressionStream so the page takes the one-frame snapshot.
import { chromium } from 'playwright';
const [ID, PIN] = process.argv.slice(2);
const THROTTLE = Number(process.env.THROTTLE || 1);
const b = await chromium.launch();
const ctx = await b.newContext({ viewport: { width: 1280, height: 900 } });
await ctx.addInitScript((plain) => {
  try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {}
  if (plain) { try { delete window.DecompressionStream; window.DecompressionStream = undefined; } catch (_) {} }
  window.__long = [];
  try { new PerformanceObserver((l) => { for (const e of l.getEntries()) window.__long.push([e.startTime, e.duration]); }).observe({ type: 'longtask', buffered: true }); } catch (_) {}
}, process.env.PLAIN === '1');
const page = await ctx.newPage();
const cdp = await ctx.newCDPSession(page);
await cdp.send('Emulation.setCPUThrottlingRate', { rate: THROTTLE });
// NET: a phone's network. slow4g is Lighthouse's (1.6 Mbps down, 750 kbps up,
// 150 ms round trip); fast4g 9 Mbps / 1.5 Mbps / 60 ms. Unset: the local link.
const NETS = { slow4g: [1.6e6, 0.75e6, 150], fast4g: [9e6, 1.5e6, 60] };
const net = NETS[process.env.NET || ''];
if (net) {
  await cdp.send('Network.enable');
  await cdp.send('Network.emulateNetworkConditions', { offline: false, downloadThroughput: net[0] / 8, uploadThroughput: net[1] / 8, latency: net[2] });
}
let parts = 0, wsOpen = 0, firstSnap = 0, lastSnap = 0, snapBytes = 0, pageBytes = 0;
page.on('response', async (r) => { if (r.url().startsWith('http://127.0.0.1:18080/') && !r.url().includes('/ws')) { try { pageBytes += (await r.body()).length; } catch (_) {} } });
page.on('websocket', (ws) => {
  if (!wsOpen) wsOpen = Date.now() - t0;
  ws.on('framereceived', (f) => {
    if (typeof f.payload !== 'string') return;
    const snap = f.payload.includes('"snapshot_part"') || (f.payload.length > 50000 && f.payload.includes('"data"'));
    if (f.payload.includes('"snapshot_part"')) parts++;
    if (snap) { if (!firstSnap) firstSnap = Date.now() - t0; lastSnap = Date.now() - t0; snapBytes += f.payload.length; }
  });
});
const t0 = Date.now();
await page.goto(`http://127.0.0.1:18080/?s=${ID}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
const domMs = Date.now() - t0;
const look = () => page.evaluate(() => {
  if (typeof term === 'undefined' || !term.buffer) return null;
  const buf = term.buffer.active, rows = [];
  for (let i = buf.viewportY; i < buf.viewportY + term.rows; i++) rows.push(buf.getLine(i)?.translateToString(true) || '');
  // A paced snapshot's history lands all at once: until then (snapLive)
  // the line count holds still without the history being in.
  return { screen: rows.join('\n'), lines: buf.length, pending: typeof snapLive !== 'undefined' && !!snapLive };
}).catch(() => null);
let a = 0, bk = 0, c = 0, lastLines = -1, stableSince = 0, typed = false;
for (let t = 0; t < 180000; t += 25) {
  const s = await look();
  const now = Date.now() - t0;
  if (s) {
    if (!a && /READY\$/.test(s.screen)) {
      a = now;
      await page.keyboard.type('z');
      typed = true;
    }
    if (typed && !bk && /READY\$ z/.test(s.screen)) bk = now;
    if (s.lines !== lastLines || s.pending) { lastLines = s.lines; stableSince = now; }
    if (a && bk && now - stableSince > 3000) { c = stableSince; break; }
  }
  await page.waitForTimeout(25);
}
// The keystroke taken back, so the next viewer finds the prompt as it was.
await page.keyboard.press('Backspace');
await page.waitForTimeout(1500);
const lines = await page.evaluate(() => {
  const buf = term.buffer.active, seen = new Set();
  for (let i = 0; i < buf.length; i++) { const m = (buf.getLine(i)?.translateToString(true) || '').match(/^line (\d+) /); if (m) seen.add(m[1]); }
  return seen.size;
});
const long = await page.evaluate(() => window.__long);
const inWin = long.filter(([s]) => s <= (c || 1e9));
const blocked = Math.round(inWin.reduce((x, [, d]) => x + Math.max(0, d - 50), 0));
const maxLong = Math.round(Math.max(0, ...inWin.map(([, d]) => d)));
console.log(JSON.stringify({ page_loaded_ms: domMs, page_kb: Math.round(pageBytes / 1024), ws_open_ms: wsOpen, snapshot_first_frame_ms: firstSnap, snapshot_last_frame_ms: lastSnap, snapshot_kb: Math.round(snapBytes / 1024), net: process.env.NET || 'local', throttle: THROTTLE, path: process.env.PLAIN === '1' ? 'plain' : 'parts', budget: process.env.BUDGET, a_screen_ms: a, b_echo_ms: bk, b_minus_a_ms: bk && a ? bk - a : null, c_history_ms: c, history_lines: lines, parts, blocking_ms: blocked, longest_task_ms: maxLong }));
await b.close();
