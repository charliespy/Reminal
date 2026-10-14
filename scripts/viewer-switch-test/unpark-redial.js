// A session that comes back live by an unpark alone has its first close redialled at once,
// as one that came back by 'connected' does.
const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  const S = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const live = () => p.waitForFunction(() => agentLive && cryptoKey && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 20000 });
  const go = (s) => p.evaluate((s) => sessdockConnect({ id: s.id, pin: s.pin }), s);
  await p.goto(S[0].url); await live();
  await go(S[1]); await live();
  await p.evaluate(() => { redialNow = false; }); // as after a close that used it up
  await go(S[0]); await live();
  console.log(JSON.stringify({ warm: await p.evaluate(() => keyExID === 'warm'), redialNowAfterUnpark: await p.evaluate(() => redialNow) }));
  await b.close();
})();
