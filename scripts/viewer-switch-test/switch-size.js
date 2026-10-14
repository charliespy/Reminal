// After a switch, the session switched to is told this screen's size, and a
// full-screen program in it is drawn to the grid's last row — warm or fresh,
// on a phone, a tall tablet and a desktop. See README.md (Switch size).
//   node switch-size.js joins.txt <phone|tablet|desktop>
// Two sessions; another viewer leaves the second's PTY short (1000×380)
// running switch-size-tui.sh (TUI_SH, default /p/switch-size-tui.sh), which
// draws BOT-<rows> on its last row and again on every SIGWINCH.
const { chromium } = require('playwright');
const fs = require('fs');
const PROFILES = {
  phone: { viewport: { width: 412, height: 915 }, deviceScaleFactor: 2.625, isMobile: true, hasTouch: true },
  tablet: { viewport: { width: 757, height: 840 }, deviceScaleFactor: 2.6, isMobile: true, hasTouch: true },
  desktop: { viewport: { width: 1280, height: 860 } },
};
const TUI_SH = process.env.TUI_SH || '/p/switch-size-tui.sh';
(async () => {
  const [A, B] = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const prof = process.argv[3] || 'phone';
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const init = () => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} };
  const shows = (p, m) => p.waitForFunction((m) => !document.getElementById('terminal').classList.contains('stale') && (document.querySelector('.xterm-rows')?.innerText || '').includes(m), m, { timeout: 20000, polling: 'raf' });
  // Another viewer, wide and low, leaves B's PTY short with the program running.
  async function shortenB(start) {
    const o = await (await b.newContext({ viewport: { width: 1000, height: 380 } })).newPage();
    await o.addInitScript(init);
    await o.goto(B.url); await shows(o, start ? 'PIPE-OK' : 'BOT-'); await o.waitForTimeout(1000);
    if (start) { await o.keyboard.type('bash ' + TUI_SH + '\n'); await shows(o, 'BOT-'); await o.waitForTimeout(500); }
    const size = await o.evaluate(() => term.cols + 'x' + term.rows);
    await o.context().close();
    return size;
  }
  const out = { prof, otherLeft: await shortenB(true), switches: [] };
  const ctx = await b.newContext(PROFILES[prof]);
  await ctx.addInitScript(init);
  const p = await ctx.newPage();
  await p.goto(A.url); await shows(p, 'PIPE-OK'); await p.waitForTimeout(1500);
  async function sw(s, label) {
    const isB = s === B;
    await p.evaluate(() => {
      window.__fr = []; window.__since = Date.now(); const t0 = performance.now();
      const tick = () => {
        const sn = TermSize.snapshot();
        window.__fr.push({ t: Math.round(performance.now() - t0), rows: term.rows, measR: sn.measR,
          stale: document.getElementById('terminal').classList.contains('stale'),
          bot: [...document.querySelectorAll('.xterm-rows > div')].findIndex(d => /BOT-/.test(d.textContent)) });
        if (performance.now() - t0 < 3000) requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    });
    await p.evaluate(([id, pin]) => sessdockConnect({ id, pin }), [s.id, s.pin]);
    await shows(p, isB ? 'BOT-' : 'PIPE-OK');
    await p.waitForTimeout(3200);
    const r = await p.evaluate(() => ({ fr: window.__fr, warm: keyExID === 'warm', sent: window.__size().events.filter(e => e.t >= window.__since && e.kind === 'send').map(e => e.c + 'x' + e.r) }));
    const grid = r.fr[r.fr.length - 1].measR;
    const drawn = r.fr.filter(f => !f.stale && f.bot >= 0);
    const res = { label, warm: r.warm, grid, sent: r.sent, gridShortFrames: r.fr.filter(f => f.rows < f.measR).length };
    if (isB) {
      res.firstDrawnRow = drawn.length ? drawn[0].bot : null;
      res.fullAtMs = (drawn.find(f => f.bot === f.rows - 1) || {}).t ?? null;
      // Warm: the first frame drawn is full. Fresh: its screen is asked for
      // with the handshake, before a key exists to send the size under, so
      // its first frame may be the old size — full once the size lands.
      res.ok = res.gridShortFrames === 0 && r.sent.length > 0 && res.fullAtMs !== null && (!r.warm || res.firstDrawnRow === grid - 1);
    } else res.ok = res.gridShortFrames === 0 && r.sent.length > 0;
    out.switches.push(res);
  }
  await sw(B, 'A→B fresh');
  await sw(A, 'B→A warm');
  await shortenB(false);
  await sw(B, 'A→B warm');
  out.ok = out.switches.every(s => s.ok);
  console.log(JSON.stringify(out));
  await b.close();
})();
