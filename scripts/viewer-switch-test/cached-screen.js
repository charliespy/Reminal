const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  const S = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const [A, B, C, D] = S;
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const rows = () => p.evaluate(() => document.querySelector('.xterm-rows')?.innerText || '');
  const all = () => p.evaluate(() => { const b = term.buffer.active, o = []; for (let y = 0; y < b.length; y++) o.push(b.getLine(y).translateToString(true)); return o.join('\n'); });
  const stale = () => p.evaluate(() => document.getElementById('terminal').classList.contains('stale'));
  const go = (s, pin) => p.evaluate(([id, pin]) => sessdockConnect({ id, pin }), [s.id, pin ?? s.pin]);
  const live = (m) => p.waitForFunction((m) => !document.getElementById('terminal').classList.contains('stale') && (document.querySelector('.xterm-rows')?.innerText || '').includes(m), m, { timeout: 15000, polling: 'raf' });
  const mark = async () => (await rows()).match(/PIPE-OK-\d+/)[0];
  const R = {};
  await p.goto(A.url); await live('PIPE-OK'); const mA = await mark();
  await go(B); await live('PIPE-OK'); const mB = await mark();
  // 1. a cached switch back to A: shown at once, then live; no "Disconnected" line carried over
  await go(A); await p.waitForTimeout(30); R.cachedShown = (await stale()) && (await rows()).includes(mA);
  await live(mA); await p.waitForTimeout(800);
  R.noDisconnectedLine = !(await all()).includes('Disconnected from');
  // 2. wrong PIN for B: the remembered screen comes down and is forgotten.
  // A fresh dial: a warm socket (one already proven) would not ask for a PIN.
  await p.evaluate(() => { if (typeof closeAllWarm === 'function') closeAllWarm(); });
  await go(B, '000000');
  await p.waitForFunction(() => /PIN mismatch|failed/i.test(document.getElementById('status')?.textContent || ''), null, { timeout: 15000 });
  await p.waitForTimeout(200);
  R.wrongPinCleared = !(await stale()) && !(await all()).includes(mB);
  R.wrongPinForgotten = await p.evaluate((id) => !screenCache.has(id), B.id);
  // back to A, then B with the right PIN: no remembered screen for B any more
  await go(A); await live(mA);
  await go(B); await p.waitForTimeout(30); R.noStaleAfterWrongPin = !(await stale());
  await live('PIPE-OK');
  // 3. forgetSession(A): no remembered screen for A
  await p.evaluate((id) => forgetSession(id), A.id);
  R.forgotten = await p.evaluate((id) => !screenCache.has(id), A.id);
  await go(A); await p.waitForTimeout(30); R.noStaleAfterForget = !(await stale()); await live(mA);
  // 4. an ended session: C live, D live, kill C, back to C → the stale comes down
  // (C's warm socket, if any, closes with its agent; the switch back dials afresh)
  await go(C); await live('PIPE-OK'); const mC = await mark();
  await go(D); await live('PIPE-OK');
  fs.writeFileSync('/p/kill-' + C.id, '');
  await p.waitForTimeout(3000);
  await go(C); await p.waitForTimeout(30); R.endedShownFirst = await stale();
  try { await p.waitForFunction(() => !document.getElementById('terminal').classList.contains('stale'), null, { timeout: 20000 }); R.endedCleared = !(await all()).includes(mC); } catch { R.endedCleared = false; }
  R.endedStatus = await p.evaluate(() => document.getElementById('status')?.textContent);
  // 5. pagehide clears every remembered screen
  R.beforePagehide = await p.evaluate(() => screenCache.size);
  await p.evaluate(() => window.dispatchEvent(new Event('pagehide')));
  R.afterPagehide = await p.evaluate(() => screenCache.size);
  console.log(JSON.stringify(R));
  await b.close();
})();
