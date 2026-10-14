const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  const S = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const sent = [];
  p.on('websocket', ws => ws.on('framesent', f => { try { const m = JSON.parse(f.payload); if (['auth', 'pake_init', 'own_init', 'park', 'unpark', 'resume'].includes(m.type)) sent.push(m.type); } catch {} }));
  const rows = () => p.evaluate(() => document.querySelector('.xterm-rows')?.innerText || '');
  const live = async (m, where) => { try { await p.waitForFunction((m) => !document.getElementById('terminal').classList.contains('stale') && agentLive && (document.querySelector('.xterm-rows')?.innerText || '').includes(m), m, { timeout: 15000, polling: 'raf' }); } catch (e) {
    console.log('TIMEOUT', where || '', m, JSON.stringify(await p.evaluate(() => ({ sid: sessionId, live: agentLive, key: !!cryptoKey, ws: ws && ws.readyState, cls: document.getElementById('terminal').className, status: document.getElementById('status')?.textContent, rows: (document.querySelector('.xterm-rows')?.innerText || '').slice(0, 200), warm: [...warmSockets.entries()].map(([k, e]) => k + ':' + e.parked + ':' + (e.ws && e.ws.readyState)) }))), 'sent=' + sent.slice(-8).join(',')); throw e; } };
  const go = (s) => p.evaluate((s) => sessdockConnect({ id: s.id, pin: s.pin }), s);
  const mark = async () => (await rows()).match(/PIPE-OK-\d+/)[0];
  const R = { switches: [] };
  await p.goto(S[0].url); await live('PIPE-OK'); const marks = [await mark()];
  for (let i = 1; i < S.length; i++) { await go(S[i]); await live('PIPE-OK'); marks.push(await mark()); }
  R.warmAfterTour = await p.evaluate(() => [...warmSockets.keys()].length);
  await p.waitForTimeout(500);
  await p.evaluate(() => { window.__rw = []; const orig = resumeWarm; resumeWarm = (s) => { const e = warmSockets.get(s); const r = orig(s); window.__rw.push([s, [...warmSockets.keys()].join('/'), !!e, e && e.parked, e && e.ws && e.ws.readyState, r]); return r; }; const ow = warmFailed; warmFailed = (w) => { window.__rw.push(['FAILED', w]); return ow(w); }; });
  R.warmState = await p.evaluate(() => [...warmSockets.values()].map(e => e.sid + ':' + e.parked + ':' + (e.ws && e.ws.readyState)));
  // switches among the last 3 visited + current: all warm
  const ring = S.slice(-4);
  for (let r = 0; r < 3; r++) for (const s of ring) {
    if (await p.evaluate(() => sessionId) === s.id) continue;
    sent.length = 0;
    const t0 = Date.now();
    await go(s);
    await live(marks[S.indexOf(s)]);
    R.switches.push({ ms: Date.now() - t0, frames: sent.join(',') });
  }
  // typing works after a warm switch
  const out = []; p.on('websocket', ws => ws.on('framesent', f => out.push(String(f.payload).slice(0, 60))));
  await p.evaluate(() => sendInput('echo TYPED-OK\r'));
  try { await p.waitForFunction(() => (document.querySelector('.xterm-rows')?.innerText || '').includes('TYPED-OK'), null, { timeout: 10000 }); } catch (e) {
    console.log('TYPE-FAIL', JSON.stringify(await p.evaluate(() => ({ sid: sessionId, live: agentLive, key: !!cryptoKey, seal: !!sealState, ws: ws && ws.readyState, held: heldInput.length, focus: document.activeElement && document.activeElement.className, rows: (document.querySelector('.xterm-rows')?.innerText || '').slice(-200) }))), 'sent:', out.slice(0, 6).join(' || ')); throw e; }
  R.typedAfterWarm = true;
  R.rw = await p.evaluate(() => window.__rw.slice(0, 6));
  fs.writeFileSync('/p/warm-ids.txt', JSON.stringify({ current: await p.evaluate(() => sessionId), warm: await p.evaluate(() => [...warmSockets.keys()]) }));
  // the agent of a warm session restarts: its parked socket is closed; the switch back dials afresh and works
  const victim = await p.evaluate(() => [...warmSockets.keys()][0]);
  R.restartSaid = (await (await fetch('http://127.0.0.1:18999/restart/' + victim)).text()).trim().slice(0, 80);
  console.log('STAGE restart', victim); await p.waitForFunction((v) => !warmSockets.has(v), victim, { timeout: 20000 }).catch(async (e) => { console.log('STAGE stuck', JSON.stringify(await p.evaluate(() => [...warmSockets.values()].map(e => e.sid + ':' + e.parked + ':' + (e.ws && e.ws.readyState))))); fs.writeFileSync('/p/stuck', victim); await p.waitForTimeout(30000); console.log('STAGE stuck+30s', JSON.stringify(await p.evaluate(() => [...warmSockets.values()].map(e => e.sid + ':' + e.parked + ':' + (e.ws && e.ws.readyState))))); throw e; });
  R.parkedClosedOnRestart = true;
  sent.length = 0;
  await go(S.find(s => s.id === victim));
  await p.waitForFunction(() => agentLive && !document.getElementById('terminal').classList.contains('stale'), null, { timeout: 20000 }).catch(async (e) => { console.log('STAGE back-stuck', JSON.stringify(await p.evaluate(() => ({ sid: sessionId, live: agentLive, key: !!cryptoKey, ws: ws && ws.readyState, cls: document.getElementById('terminal').className, status: document.getElementById('status')?.textContent }))), 'sent=' + sent.join(',')); throw e; });
  R.afterRestartFrames = sent.join(',');
  R.beforePagehide = await p.evaluate(() => warmSockets.size);
  await p.evaluate(() => window.dispatchEvent(new Event('pagehide')));
  R.afterPagehide = await p.evaluate(() => warmSockets.size);
  const ms = R.switches.map(x => x.ms).sort((a, b) => a - b);
  R.median = ms[ms.length >> 1]; R.max = ms[ms.length - 1];
  R.switchFrames = [...new Set(R.switches.map(x => x.frames))];
  delete R.switches;
  console.log(JSON.stringify(R));
  await b.close();
})();
