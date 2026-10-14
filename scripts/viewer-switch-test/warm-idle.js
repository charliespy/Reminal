const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  const S = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const secs = +process.argv[3];
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const live = () => p.waitForFunction(() => agentLive && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 15000 });
  await p.goto(S[0].url); await live();
  for (let i = 1; i < S.length; i++) { await p.evaluate((s) => sessdockConnect({ id: s.id, pin: s.pin }), S[i]); await live(); }
  console.log('PHASE warm', await p.evaluate(() => warmSockets.size)); fs.writeFileSync('/p/phase', 'warm');
  await p.waitForTimeout(secs * 1000);
  await p.evaluate(() => closeAllWarm());
  console.log('PHASE alone', await p.evaluate(() => warmSockets.size)); fs.writeFileSync('/p/phase', 'alone');
  await p.waitForTimeout(secs * 1000);
  fs.writeFileSync('/p/phase', 'done');
  await b.close();
})();
