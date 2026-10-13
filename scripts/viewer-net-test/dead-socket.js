// A network change kills the viewer's socket silently. Rows: keys typed into it, the online event.
const { chromium } = require('playwright');
const fs = require('fs');
const until = (f) => new Promise(r => { const t = setInterval(() => { if (fs.existsSync(f)) { clearInterval(t); r(); } }, 100); });
(async () => {
  const [S] = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const rows = () => p.evaluate(() => { const b = term.buffer.active, o = []; for (let y = 0; y < b.length; y++) o.push(b.getLine(y).translateToString(true)); return o.join('\n'); });
  const live = (ms) => p.waitForFunction(() => agentLive && cryptoKey && ws && ws.readyState === 1, null, { timeout: ms });
  await p.goto(S.url); await live(20000);
  await p.waitForFunction(() => (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 20000 });
  const R = {};
  const drop = async (n) => { fs.writeFileSync('/p/drop', ''); await until('/p/dropped'); fs.unlinkSync('/p/dropped'); };
  // 1. keys typed into the dead socket
  await drop();
  const t0 = Date.now();
  await p.evaluate(() => sendInput('echo AAA\r'));
  const ok1 = await p.waitForFunction(() => /may not have reached the session/.test((() => { const b = term.buffer.active, o = []; for (let y = 0; y < b.length; y++) o.push(b.getLine(y).translateToString(true)); return o.join('\n'); })()) || /AAA\n/.test(document.querySelector('.xterm-rows')?.innerText || ''), null, { timeout: 30000 }).then(() => true, () => false);
  R.keysAccountedFor = ok1; R.noticeAfterMs = Date.now() - t0;
  R.redialled = await live(30000).then(() => true, () => false); R.liveAgainAfterMs = Date.now() - t0;
  await p.evaluate(() => sendInput('echo BBB\r'));
  R.keysAfterRedialArrive = await p.waitForFunction(() => (document.querySelector('.xterm-rows')?.innerText || '').includes('BBB\n'), null, { timeout: 15000 }).then(() => true, () => false);
  // keys typed while it was dead and the redial was under way: held, then delivered
  // 2. the network comes back (online): probed, found dead, redialled
  await drop();
  const t1 = Date.now();
  await p.evaluate(() => window.dispatchEvent(new Event('online')));
  await p.waitForFunction(() => !ws || ws.readyState !== 1 || !agentLive, null, { timeout: 15000 }).catch(() => {});
  R.onlineRedialled = await live(30000).then(() => true, () => false); R.onlineLiveAfterMs = Date.now() - t1;
  R.text = (await rows()).split('\n').filter(l => /AAA|BBB|reminal\]/.test(l)).slice(-6);
  fs.writeFileSync('/p/done', '');
  console.log(JSON.stringify(R));
  await b.close();
})();
