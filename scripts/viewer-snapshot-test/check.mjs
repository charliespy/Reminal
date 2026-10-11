// A viewer opens the session fresh and waits for its history to land; it
// counts the distinct "line N" its terminal holds (run.sh).
import { chromium } from 'playwright';
const [ID, PIN, WANT] = process.argv.slice(2);
const b = await chromium.launch();
const ctx = await b.newContext({ viewport: { width: 2600, height: 900 } });
await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
const p = await ctx.newPage();
const parts = [];
p.on('websocket', (ws) => ws.on('framereceived', (f) => {
  if (typeof f.payload === 'string' && f.payload.includes('"snapshot_part"')) parts.push(f.payload.length);
}));
await p.goto(`http://127.0.0.1:18080/?s=${ID}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
const count = () => p.evaluate(() => {
  const buf = term.buffer.active, seen = new Set();
  for (let i = 0; i < buf.length; i++) {
    const m = (buf.getLine(i)?.translateToString(true) || '').match(/^line (\d+) /);
    if (m) seen.add(m[1]);
  }
  return seen.size;
});
let n = 0, still = 0;
for (let t = 0; t < 60000 && still < 6; t += 500) {
  await p.waitForTimeout(500);
  const c = await count().catch(() => 0);
  still = c > 0 && c === n ? still + 1 : 0;
  n = c;
}
const tail = await p.evaluate(() => {
  const buf = term.buffer.active, nums = [];
  for (let i = 0; i < buf.length; i++) { const m = (buf.getLine(i)?.translateToString(true) || '').match(/^line (\d+) /); if (m) nums.push(+m[1]); }
  let ordered = true;
  for (let i = 1; i < nums.length; i++) if (nums[i] !== nums[i - 1] + 1) { ordered = false; break; }
  return { first: nums[0], last: nums[nums.length - 1], ordered, rows: buf.length };
});
const want = +WANT;
const ok = (want ? n >= want : n > 0) && tail.ordered && tail.last === 4000;
console.log(`viewer holds ${n}/4000 lines (lines ${tail.first}..${tail.last}, in order: ${tail.ordered}; ${tail.rows} rows); snapshot parts received: ${parts.length}${parts.length ? ' (' + parts.map((x) => Math.round(x / 1024) + ' KiB').join(', ') + ')' : ''}   ${ok ? 'PASS' : 'FAIL'}`);
await b.close();
process.exit(ok ? 0 : 1);
