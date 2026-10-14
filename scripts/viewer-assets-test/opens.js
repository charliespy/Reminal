// Open → prompt, three times in one browser (its cache kept): first, repeat, third.
const { chromium, devices } = require('playwright');
const fs = require('fs');
(async () => {
  const [S] = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => ({ url: u }));
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const ctx = await b.newContext({ ...devices['Pixel 7'] });
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
  const R = { opens: [], bytes: [] };
  for (let i = 0; i < 3; i++) {
    const p = await ctx.newPage();
    let bytes = 0;
    p.on('response', async r => { try { if (r.url().startsWith('http://localhost')) { const h = r.headers()['content-length']; bytes += h ? +h : 0; } } catch (_) {} });
    const cdp = await ctx.newCDPSession(p); await cdp.send('Emulation.setCPUThrottlingRate', { rate: 4 });
    const t0 = Date.now();
    await p.goto(S.url);
    await p.waitForFunction(() => typeof agentLive !== 'undefined' && agentLive && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 60000, polling: 100 });
    R.opens.push(Date.now() - t0); R.bytes.push(bytes);
    if (i === 2) R.swCaches = await p.evaluate(async () => (await caches.keys()).length);
    await p.close();
  }
  console.log(JSON.stringify(R));
  await b.close();
})();
