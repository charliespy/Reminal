// What a viewer already working feels while someone else joins (run.sh
// SCRIPT=fan.mjs CASE=…): a viewer on a throttled link, caught up, types a
// key every 300 ms; after the 4th a fast viewer opens the session
// (CASE=join) or nobody does (CASE=nojoin). Prints the slow viewer's echo
// times before and after. With nobody joining every echo must be within
// ECHO_MS; with a join it is measured only — a joiner's snapshot parts reach
// every viewer on the session, so a slow one waits behind them (as before the
// paced join; a follow-up). Built from typing.mjs.
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

// (4) A slow viewer already attached and caught up types a key every 300 ms;
// after the 4th key a fast viewer joins (CASE=join) or not (CASE=nojoin).
// The slow one's echo times are compared before and after.
const CASE = process.env.CASE || 'join';
await until(page, () => typeof snapLive === 'undefined' || !snapLive);
await page.waitForTimeout(2000);
const word = 'echo ABCDEFGHIJKLMNOPQRS';
let other = null;
for (let i = 0; i < word.length; i++) {
  if (i === 4 && CASE === 'join') other = open(false);
  await page.keyboard.type(word[i]);
  await page.waitForTimeout(300);
}
if (other) await other;
await page.waitForTimeout(3000);
const keys = await page.evaluate(() => window.__keys.map((k) => ({ ch: k.ch, echoMs: k.echo ? Math.round(k.echo - k.at) : null })));
const before = keys.slice(0, 4).map((k) => k.echoMs), after = keys.slice(4).map((k) => k.echoMs);
const worst = (a) => Math.max(...a.map((x) => x ?? 99999));
console.log(JSON.stringify({ CASE, before, after, worstBefore: worst(before), worstAfter: worst(after) }));
const ok = CASE !== 'nojoin' || worst(keys.map((k) => k.echoMs)) <= ECHO_MS;
console.log((ok ? 'PASS  ' : 'FAIL  ') + CASE + (CASE === 'nojoin' ? `: every echo within ${ECHO_MS} ms` : ': measured only'));
await b.close();
process.exit(ok ? 0 : 1);
