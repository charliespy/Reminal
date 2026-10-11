// Typing while a long session's history is still coming (run.sh SCRIPT=
// typing.mjs): a viewer on a throttled link (NET, THROTTLE) opens the session
// and, from the moment its prompt is painted, types a key every 300 ms. Each
// key's echo is timed (from the key to the echo reaching the page), noting
// whether the history was still downloading; then it presses Enter. Once all
// is in, its terminal must hold exactly what a viewer opening the session
// afresh is given: every line, in order, nothing lost or doubled.
import { chromium } from 'playwright';
const [ID, PIN] = process.argv.slice(2);
const THROTTLE = Number(process.env.THROTTLE || 4);
const NETS = { slow4g: [1.6e6, 0.75e6, 150], fast4g: [9e6, 1.5e6, 60] };
const net = NETS[process.env.NET || 'slow4g'];
const ECHO_MS = Number(process.env.ECHO_MS || 500);
const URL = `http://127.0.0.1:18080/?s=${ID}#p=${PIN}`;
const b = await chromium.launch();
const open = async (throttled) => {
  const ctx = await b.newContext({ viewport: { width: 1280, height: 900 } });
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
  const page = await ctx.newPage();
  if (throttled) {
    const cdp = await ctx.newCDPSession(page);
    await cdp.send('Emulation.setCPUThrottlingRate', { rate: THROTTLE });
    await cdp.send('Network.enable');
    await cdp.send('Network.emulateNetworkConditions', { offline: false, downloadThroughput: net[0] / 8, uploadThroughput: net[1] / 8, latency: net[2] });
  }
  await page.goto(URL, { waitUntil: 'domcontentloaded' });
  return page;
};
// Everything the terminal holds, line by line (text only), and how many
// distinct "line N" it has, in order or not.
const contents = (page) => page.evaluate(() => {
  const buf = term.buffer.active, text = [], nums = [];
  for (let i = 0; i < buf.length; i++) {
    // Trailing spaces aside: a prompt written live keeps its space, the
    // same row repainted by a snapshot does not; they look the same.
    const l = (buf.getLine(i)?.translateToString(true) || '').replace(/\s+$/, '');
    text.push(l);
    const m = l.match(/^line (\d+) /);
    if (m) nums.push(+m[1]);
  }
  while (text.length && text[text.length - 1] === '') text.pop();
  let ordered = nums.length > 0;
  for (let i = 1; i < nums.length; i++) if (nums[i] !== nums[i - 1] + 1) { ordered = false; break; }
  return { text, lines: nums.length, distinct: new Set(nums).size, ordered, first: nums[0], last: nums[nums.length - 1] };
});
const until = async (page, fn, arg, ms = 180000) => page.waitForFunction(fn, arg, { timeout: ms, polling: 50 });

const page = await open(true);
await until(page, () => typeof term !== 'undefined' && typeof enqueueData === 'function');
// When each key was typed, and when output carrying it reached the page.
await page.evaluate(() => {
  window.__keys = [];
  term.onData((d) => { for (const ch of d) window.__keys.push({ ch, at: performance.now(), echo: 0 }); });
  const orig = enqueueData;
  enqueueData = (bytes) => {
    const s = new TextDecoder().decode(bytes), now = performance.now();
    for (const k of window.__keys) if (!k.echo && s.includes(k.ch)) { k.echo = now; break; }
    return orig(bytes);
  };
});
await until(page, () => {
  const buf = term.buffer.active;
  for (let i = buf.viewportY; i < buf.viewportY + term.rows; i++) if (/READY\$/.test(buf.getLine(i)?.translateToString(true) || '')) return true;
  return false;
});
// What is on screen each frame from here: the prompt must never leave it —
// the history's repaint is not to be seen.
await page.evaluate(() => {
  window.__frames = { n: 0, gone: 0 };
  const tick = () => {
    const shown = term.element.querySelector('.xterm-rows');
    window.__frames.n++;
    if (!/READY\$/.test(shown ? shown.textContent : '')) window.__frames.gone++;
    if (!window.__framesStop) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
});
const word = 'echo PACED';
const log = [];
for (const ch of word) {
  const before = await page.evaluate(() => { const buf = term.buffer.active; let n = 0; for (let i = 0; i < buf.length; i++) if (/^line \d+ /.test(buf.getLine(i)?.translateToString(true) || '')) n++; return n; });
  await page.keyboard.type(ch);
  log.push({ ch, historyLinesWhenTyped: before });
  await page.waitForTimeout(300);
}
await page.keyboard.press('Enter');
await until(page, () => { const buf = term.buffer.active; for (let i = 0; i < buf.length; i++) if ((buf.getLine(i)?.translateToString(true) || '') === 'PACED') return true; return false; });
// All in: no snapshot under way, and the line count no longer growing.
await until(page, () => typeof snapLive === 'undefined' || !snapLive);
let mine, still = 0;
for (let prev = -1; still < 6;) { await page.waitForTimeout(500); mine = await contents(page); still = mine.lines === prev ? still + 1 : 0; prev = mine.lines; }
const frames = await page.evaluate(() => { window.__framesStop = true; const b = term.buffer.active; return { ...window.__frames, atBottom: b.viewportY === b.baseY }; });
const keys = await page.evaluate(() => window.__keys.map((k) => ({ ch: k.ch, echoMs: k.echo ? Math.round(k.echo - k.at) : null })));
for (let i = 0; i < log.length; i++) Object.assign(log[i], keys[i] || {});

const ref = await open(false);
await until(ref, () => typeof term !== 'undefined');
let theirs; still = 0;
for (let prev = -1; still < 6;) { await ref.waitForTimeout(500); theirs = await contents(ref); still = theirs.lines === prev && theirs.text.includes('PACED') ? still + 1 : 0; prev = theirs.lines; }
// The fresh viewer's history starts later by what was typed since (the
// history budget is of the newest bytes), so it is compared from the end:
// it must be exactly the end of this one's, and what this one holds above
// that only the older lines just before.
const off = mine.text.length - theirs.text.length;
let firstDiff = off < 0 ? 0 : -1;
for (let i = 0; firstDiff < 0 && i < theirs.text.length; i++) if (mine.text[off + i] !== theirs.text[i]) firstDiff = off + i;
const above = off > 0 ? mine.text.slice(0, off) : [];
const aboveOK = above.every((l, i) => /^line (\d+) /.test(l) && +l.match(/^line (\d+) /)[1] === theirs.first - off + i);

const during = log.filter((k) => k.historyLinesWhenTyped < 10000);
const worst = Math.max(...log.map((k) => k.echoMs ?? Infinity));
console.log(JSON.stringify({ net: process.env.NET || 'slow4g', throttle: THROTTLE, keys: log, typedDuringDownload: during.length, worstEchoMs: worst, frames, mine: { lines: mine.lines, distinct: mine.distinct, ordered: mine.ordered, first: mine.first, rows: mine.text.length }, reference: { lines: theirs.lines, first: theirs.first, rows: theirs.text.length }, extraAbove: off, firstDiff, diff: firstDiff < 0 ? null : { mine: mine.text[firstDiff], reference: theirs.text[firstDiff - Math.max(off, 0)] } }));
const checks = [
  ['keys typed while the history was still coming', during.length >= 3],
  [`every echo within ${ECHO_MS} ms`, log.every((k) => k.echoMs != null && k.echoMs <= ECHO_MS)],
  ['the prompt on screen in every frame (the repaint never seen), the view at the bottom', frames.n > 0 && frames.gone === 0 && frames.atBottom],
  ['the history in order, each line once, up to the last', mine.lines === mine.distinct && mine.ordered && mine.last === 10000],
  ['the terminal is exactly what a fresh viewer is given (and, above it, only the older lines just before)', firstDiff < 0 && off >= 0 && aboveOK],
];
for (const [n, ok] of checks) console.log((ok ? 'PASS  ' : 'FAIL  ') + n);
await b.close();
process.exit(checks.every(([, ok]) => ok) ? 0 : 1);
