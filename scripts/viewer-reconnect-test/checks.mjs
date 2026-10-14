// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
import { chromium, devices } from 'playwright';
const BASE = 'http://127.0.0.1:18080';
const [S, PIN] = process.argv.slice(2);
const b = await chromium.launch();
const out = [];
let failed = 0;
const verdict = (ok) => { if (!ok) failed++; return ok ? 'PASS' : 'FAIL'; };

// Every socket the page opens goes through here: `refuse` closes the next
// one before it reaches the relay (a redial that fails), `busy` answers the
// next pake_init with pake_busy instead of passing it on.
async function page(opts = {}) {
  const ctx = await b.newContext({ ...devices['Pixel 7'] });
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
  const p = await ctx.newPage();
  const ctl = { refuse: 0, busy: opts.busy || 0, dials: [], wsAt: 0 };
  await p.routeWebSocket(/\/ws\//, ws => {
    ctl.dials.push(Date.now());
    if (ctl.refuse > 0) { ctl.refuse--; ws.close(); return; }
    const server = ws.connectToServer();
    ws.onMessage(m => {
      if (ctl.busy > 0 && typeof m === 'string' && m.includes('"pake_init"')) {
        try {
          const ex = JSON.parse(m).ex_id;
          ctl.busy--;
          ws.send(JSON.stringify({ type: 'pake_busy', ex_id: ex, retry_ms: 1000 }));
          return;
        } catch (_) {}
      }
      server.send(m);
    });
  });
  p.on('websocket', () => { if (!ctl.wsAt) ctl.wsAt = Date.now(); });
  return { ctx, p, ctl };
}
const live = (p) => p.waitForFunction(() => inputLive(), null, { timeout: 60000 });

// ---- 1: the handshake's library is on its way before the socket opens ----
{
  const { ctx, p, ctl } = await page();
  let libAt = 0;
  p.on('request', r => { if (r.url().includes('ristretto255.js') && !libAt) libAt = Date.now(); });
  const t0 = Date.now();
  await p.goto(`${BASE}/?s=${S}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
  await live(p);
  const open = Date.now() - t0;
  const ok = libAt > 0 && libAt <= ctl.wsAt;
  out.push(`1  first open ${open} ms; library asked for ${libAt - t0} ms in, socket opened ${ctl.wsAt - t0} ms in   ${verdict(ok)}`);
  await ctx.close();
}

// ---- 2: a drop redials at once; a redial that fails waits the backoff ----
{
  const { ctx, p, ctl } = await page();
  await p.goto(`${BASE}/?s=${S}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
  await live(p);
  await p.waitForTimeout(1000);
  let n = ctl.dials.length, t0 = Date.now();
  await p.evaluate(() => ws.close());
  await p.waitForFunction(() => !inputLive(), null, { timeout: 10000 });
  await live(p);
  const back = Date.now() - t0, first = ctl.dials[n] - t0;
  // "At once" is within the close's own round trip, never the 1 s backoff
  // (nor a peer that never answered the close: ~10 s): under 1 s on a link
  // where a close takes ~0.3 s (the Go relay) or ~0.6 s (wrangler dev's two
  // hops), here.
  out.push(`2a dropped: dialled again ${first} ms after, live again in ${back} ms   ${verdict(first < 1000)}`);
  // Up again: a drop whose at-once redial is refused, then the backoff.
  await p.waitForTimeout(1000);
  n = ctl.dials.length; t0 = Date.now();
  ctl.refuse = 1;
  await p.evaluate(() => ws.close());
  await live(p);
  const d1 = ctl.dials[n] - t0, d2 = ctl.dials[n + 1] - ctl.dials[n];
  out.push(`2b the at-once redial refused: it dialled ${d1} ms after the drop, again ${d2} ms later   ${verdict(d1 < 1000 && d2 >= 900)}`);
  await ctx.close();
}

// ---- 3: "busy", then through: the status says connected ----
{
  const { ctx, p } = await page({ busy: 1 });
  await p.goto(`${BASE}/?s=${S}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
  await live(p);
  await p.waitForTimeout(500);
  const st = await p.evaluate(() => ({ text: statusEl.textContent, cls: statusEl.className }));
  out.push(`3  told busy, then connected: status "${st.text}" (${st.cls})   ${verdict(st.text === S && /connected/.test(st.cls) && !/warn/.test(st.cls))}`);
  await ctx.close();
}

// ---- 4: after a reconnect, the next keys echo on the prompt's line, and
// the reconnect is said just above it: with the prompt at the top of a
// cleared screen (a), on its second row (c: a scroll region of one row is
// ignored), and further down (b) ----
for (const [k, setup] of [['a', 'clear'], ['c', null], ['b', 'echo one; echo two; echo three']]) {
  const { ctx, p } = await page();
  await p.goto(`${BASE}/?s=${S}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
  await live(p);
  if (setup) {
    await p.keyboard.type(setup, { delay: 40 });
    await p.keyboard.press('Enter');
  } else {
    // The prompt on the second row exactly: the screen drawn here (a shell's
    // own output after a clear is not so exact), its keys the session's.
    await p.waitForTimeout(1000);
    await p.evaluate(() => term.write('\x1b[H\x1b[2J' + 'one\r\n' + '$ '));
  }
  await p.waitForTimeout(2000);
  const row = (d) => p.evaluate((d) => { const b = term.buffer.active; return b.getLine(b.baseY + b.cursorY + d)?.translateToString(true) || ''; }, d);
  const prompt = (await row(0)).trim();
  const at = await p.evaluate(() => term.buffer.active.cursorY);
  await p.evaluate(() => ws.close());
  await p.waitForFunction(() => !inputLive(), null, { timeout: 10000 });
  await live(p);
  await p.waitForTimeout(1500);
  await p.keyboard.type('echo MARK42', { delay: 40 });
  await p.waitForTimeout(2000);
  const line = (await row(0)).trim(), above = (await row(-1)).trim();
  const ok = prompt.length > 0 && line === prompt + ' echo MARK42' && /\[reminal\] Reconnected to /.test(above);
  out.push(`4${k} prompt "${prompt}" on row ${at}; after the reconnect the cursor's line is "${line}", the line above "${above}"   ${verdict(ok)}`);
  await ctx.close();
}

console.log(out.join('\n'));
await b.close();
process.exit(failed ? 1 : 0);
