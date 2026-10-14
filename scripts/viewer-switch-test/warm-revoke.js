// A parked (warm) socket of a device that is then revoked: closed, and no way back in.
const { chromium } = require('playwright');
const fs = require('fs');
const until = (f) => new Promise(r => { const t = setInterval(() => { if (fs.existsSync(f)) { clearInterval(t); r(); } }, 200); });
(async () => {
  const S = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const [A, B] = S;
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage(); p.on('dialog', d => d.accept());
  const sent = []; p.on('websocket', ws => ws.on('framesent', f => { try { const m = JSON.parse(f.payload); if (['own_init', 'pake_init', 'unpark', 'resume'].includes(m.type)) sent.push(m.type); } catch {} }));
  await p.goto(A.url.replace(/[?#].*/, ''));
  fs.writeFileSync('/p/enroll-W', await p.evaluate(() => ownDeviceId())); await until('/p/enrolled-W');
  const owner = (id) => p.evaluate((id) => connectOwned(id), id);
  const liveOwner = () => p.waitForFunction(() => agentLive && cryptoKey && iOwnThisMachine && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 20000 });
  await owner(A.id); await liveOwner();
  await owner(B.id); await liveOwner();
  const R = {};
  R.aParked = await p.waitForFunction((id) => warmSockets.get(id) && warmSockets.get(id).parked, A.id, { timeout: 5000 }).then(() => true, () => false);
  sent.length = 0; await owner(A.id); await liveOwner(); R.warmSwitchFrames = sent.join(','); // warm works before the revoke
  await owner(B.id); await liveOwner();
  fs.writeFileSync('/p/revoke', fs.readFileSync('/p/enroll-id-W', 'utf8')); await until('/p/revoked');
  R.parkedClosedOnRevoke = await p.waitForFunction((id) => !warmSockets.has(id), A.id, { timeout: 20000 }).then(() => true, () => false);
  R.liveCut = await p.waitForFunction(() => !cryptoKey, null, { timeout: 20000 }).then(() => true, () => false);
  sent.length = 0; await owner(A.id); await p.waitForTimeout(8000);
  R.switchBack = { frames: sent.join(','), key: await p.evaluate(() => !!cryptoKey), status: await p.evaluate(() => document.getElementById('status')?.textContent) };
  fs.writeFileSync('/p/done', '');
  console.log(JSON.stringify(R));
  await b.close();
})();
