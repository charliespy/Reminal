const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  process.on('unhandledRejection', async (e) => { console.log('DEBUG', String(e).slice(0,200), JSON.stringify(await globalThis.__p?.evaluate(() => ({ sid: sessionId, live: agentLive, own: iOwnThisMachine, status: document.getElementById('status')?.textContent })).catch(() => null))); process.exit(1); });
  const S = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage(); globalThis.__p = p;
  p.on('dialog', (d) => d.accept()); // the first-meet confirm of a machine's key
  const sent = [];
  p.on('websocket', ws => ws.on('framesent', f => { try { const m = JSON.parse(f.payload); if (['own_init', 'pake_init', 'resume'].includes(m.type)) sent.push(m.type); } catch {} }));
  await p.goto(S[0].url);
  await p.waitForFunction(() => agentLive && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 15000 });
  const id = await p.evaluate(() => ownDeviceId());
  fs.writeFileSync('/p/owner-id', id);
  while (!fs.existsSync('/p/owner-ok')) await p.waitForTimeout(200);
  // Met once with the PIN: the machine's key is then this browser's to trust.
  for (const s of S.slice(1)) {
    await p.evaluate((s) => sessdockConnect({ id: s.id, pin: s.pin }), s);
    await p.waitForFunction(() => agentLive && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 15000 });
  }
  let ok = 0, bad = 0; const how = new Set();
  for (let r = 0; r < 8; r++) for (const s of S) {
    sent.length = 0;
    await p.evaluate((id) => connectOwned(id), s.id);
    try {
      await p.waitForFunction(() => agentLive && iOwnThisMachine && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 8000 });
      ok++; how.add(sent.join(','));
    } catch { bad++; how.add('BAD:' + sent.join(',') + ':' + await p.evaluate(() => document.getElementById('status')?.textContent)); }
  }
  console.log(JSON.stringify({ ok, bad, how: [...how] }));
  await b.close();
})();
