// The page under its CSP (no 'unsafe-inline' for scripts): every policy
// violation while it loads, connects and opens what a person opens — on a
// desktop and a phone — and that its scripts came from /assets/, cached for good.
const { chromium, devices } = require('playwright');
const fs = require('fs');
(async () => {
  const [S] = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => ({ url: u }));
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const out = {};
  for (const [name, opts] of [['desktop', { viewport: { width: 1280, height: 800 } }], ['phone', devices['Pixel 7']]]) {
    const ctx = await b.newContext(opts);
    await ctx.addInitScript(() => {
      window.__csp = [];
      document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(e.violatedDirective + ' ' + (e.blockedURI || '') + ' ' + (e.sourceFile || '') + ':' + (e.lineNumber || '')));
      try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {}
    });
    const p = await ctx.newPage();
    const console_ = [];
    p.on('console', m => { if (/Content Security Policy|Refused to/i.test(m.text())) console_.push(m.text().slice(0, 200)); });
    const assets = [];
    p.on('response', r => { if (/\/assets\//.test(r.url())) assets.push({ url: r.url().split('/').pop(), status: r.status(), cache: r.headers()['cache-control'] }); });
    await p.goto(S.url);
    await p.waitForFunction(() => typeof agentLive !== 'undefined' && agentLive && cryptoKey, null, { timeout: 20000 });
    const acts = [
      () => openShare(), () => closeShare(),
      () => document.getElementById('settings-btn')?.click(), () => document.body.click(),
      () => document.getElementById('keybar-toggle')?.click(), () => document.getElementById('keybar-toggle')?.click(),
      () => document.getElementById('find-btn')?.click(),
      () => { const r = document.getElementById('recents'); if (r) r.open = true; }, () => { const r = document.getElementById('recents'); if (r) r.open = false; },
      () => { const h = document.getElementById('host'); if (h) h.open = true; }, () => { const h = document.getElementById('host'); if (h) h.open = false; },
      () => typeof renderMachines === 'function' && renderMachines(),
      () => document.getElementById('sessdock-btn')?.click(),
    ];
    for (const a of acts) { try { await p.evaluate(a); } catch (_) {} await p.waitForTimeout(250); }
    await p.evaluate(() => sendInput('echo CSP-OK\r'));
    const typed = await p.waitForFunction(() => (document.querySelector('.xterm-rows')?.innerText || '').includes('CSP-OK'), null, { timeout: 10000 }).then(() => true, () => false);
    out[name] = { violations: await p.evaluate(() => window.__csp), consoleCSP: console_, assets, typed, csp: (await (await fetch(S.url.replace(/[?#].*/, ''))).headers.get('content-security-policy')) };
    await ctx.close();
  }
  console.log(JSON.stringify(out));
  await b.close();
})();
