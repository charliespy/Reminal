// What the person does while the history is still coming must not change
// how it ends (run.sh SCRIPT=gap.mjs CASE=…): a viewer on a throttled link
// opens the session and, once its prompt is painted and the history is still
// on its way, runs one command that changes the terminal; when the history is
// in, its terminal must be exactly what a fresh viewer is given (compared from
// the end, nothing doubled). CASE: control (nothing changed), region (a
// scroll region — the one that once kept the history inside its rows),
// insert, charset, sgr, resize (both pages 760x600), alt (the alternate
// screen). By Innovator 1, from typing.mjs.
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

const CASE = process.env.CASE || 'control';
const CMDS = {
  control: "echo GAPDONE",
  region: "printf '\\033[3;10r'; echo GAPDONE",
  insert: "printf '\\033[4h'; echo GAPDONE",
  charset: "printf '\\033(0'; echo GAPDONE",
  sgr: "printf '\\033[7;31m'; echo GAPDONE",
  resize: "echo GAPDONE",
  alt: "printf '\\033[?1049h\\033[2J\\033[HALT SCREEN UP'; echo GAPDONE",
};
const inFlight = await page.evaluate(() => !!snapLive);
if (CASE === 'resize') await page.setViewportSize({ width: 760, height: 600 });
await page.keyboard.type(CMDS[CASE], { delay: 20 });
await page.keyboard.press('Enter');
const stillInFlight = await page.evaluate(() => !!snapLive);
await until(page, () => { const buf = term.buffer.active; for (let i = 0; i < buf.length; i++) if ((buf.getLine(i)?.translateToString(true) || '').includes('GAPDONE')) return true; return false; });
await until(page, () => typeof snapLive === 'undefined' || !snapLive);
let mine, still = 0;
for (let prev = -1; still < 6;) { await page.waitForTimeout(500); mine = await contents(page); still = mine.lines === prev ? still + 1 : 0; prev = mine.lines; }
const ref = await open(false);
if (CASE === 'resize') await ref.setViewportSize({ width: 760, height: 600 });
await until(ref, () => typeof term !== 'undefined');
let theirs; still = 0;
for (let prev = -1; still < 6;) { await ref.waitForTimeout(500); theirs = await contents(ref); still = theirs.lines === prev && theirs.text.some((l) => l.includes('GAPDONE')) ? still + 1 : 0; prev = theirs.lines; }
// The fresh viewer's history starts later by what was typed since (the
// history budget is of the newest bytes), so it is compared from the end:
// it must be exactly the end of this one's, and what this one holds above
// that only the older lines just before.
const off = mine.text.length - theirs.text.length;
let firstDiff = off < 0 ? 0 : -1;
for (let i = 0; firstDiff < 0 && i < theirs.text.length; i++) if (mine.text[off + i] !== theirs.text[i]) firstDiff = off + i;
const above = off > 0 ? mine.text.slice(0, off) : [];
const aboveOK = above.every((l, i) => /^line (\d+) /.test(l) && +l.match(/^line (\d+) /)[1] === theirs.first - off + i);
console.log(JSON.stringify({ CASE, inFlight, stillInFlight, mine: { lines: mine.lines, distinct: mine.distinct, ordered: mine.ordered, last: mine.last, rows: mine.text.length }, reference: { lines: theirs.lines, rows: theirs.text.length }, extraAbove: off, firstDiff, diff: firstDiff < 0 ? null : { mine: mine.text[firstDiff], reference: theirs.text[firstDiff - Math.max(off, 0)] } }));
// Not the lines' order: a scroll region or the alternate screen leaves them
// out of order in a fresh viewer too; equal to it is the test.
const ok = firstDiff < 0 && off >= 0 && aboveOK && mine.lines === mine.distinct;
console.log((ok ? 'PASS  ' : 'FAIL  ') + CASE + ': final terminal matches a fresh viewer');
await b.close();
process.exit(ok ? 0 : 1);
