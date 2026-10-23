// A device with an owner key, opening sessions by their PIN (owner-pin.sh):
//   notOwned  m2, never this device's: the owner handshake, and the PIN one
//             a moment behind it, live well under a second;
//   owned     m1, met as an owner before: the owner handshake, and no PIN one;
//   revoked   m3, met as an owner, then its owner list emptied: the owner
//             handshake, the PIN one behind it in a moment, live, and not
//             taken for an owner (no owner-only controls);
//   noPin     m2 with no PIN: said within a few seconds that the PIN gets in;
//   noKey     a browser with no owner key and no PIN: "PIN required".
// Every switch is a fresh dial (the parked sockets closed first).
const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  const M = {};
  for (const l of fs.readFileSync(process.argv[2], 'utf8').trim().split('\n')) { const [m, id, pin] = l.split(' '); M[m] = { id, pin }; }
  const R = 'http://localhost:8080';
  const b = await chromium.launch();
  const live = () => agentLive && !!cryptoKey && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK');
  const status = (p) => p.evaluate(() => document.getElementById('status')?.textContent || '');
  async function page() {
    const ctx = await b.newContext();
    await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
    const p = await ctx.newPage();
    p.on('dialog', (d) => d.accept()); // the first meeting of a machine's key
    p.sent = []; p.logs = [];
    if (process.env.DBG) p.on('console', m => { const t = m.text(); if (t.startsWith('[dbg]')) p.logs.push(Date.now() + ' ' + t.slice(6, 150)); });
    p.on('websocket', ws => ws.on('framesent', f => { try { const m = JSON.parse(f.payload); if (['own_init', 'pake_init'].includes(m.type)) p.sent.push(m.type); } catch {} }));
    await p.goto(R + (process.env.DBG ? '/?debug=true' : '/'));
    await p.waitForFunction(() => typeof connectToSession === 'function');
    return p;
  }
  // Opens one session (by PIN, or as an owner with none) and times it.
  async function open(p, s, pin, until = 15000) {
    await p.evaluate(() => closeAllWarm());
    p.sent.length = 0; p.logs.length = 0;
    const t0 = Date.now();
    await p.evaluate(([id, pin]) => pin ? connectToSession(id, pin) : connectOwned(id), [s.id, pin]);
    let ms = -1;
    try { await p.waitForFunction(live, null, { timeout: until, polling: 20 }); ms = Date.now() - t0; } catch (_) {}
    if (process.env.DBG) for (const l of p.logs) console.log('  ' + (Number(l.split(' ')[0]) - t0) + 'ms ' + l.slice(l.indexOf(' ') + 1));
    // Owner or not is read a moment after: the pin store is read in the
    // background on every socket and must not have made it an owner again.
    await p.waitForTimeout(500);
    return { ms, sent: p.sent.join(','), owner: await p.evaluate(() => iOwnThisMachine) };
  }
  const out = {};

  const k = await page();
  await k.evaluate(([id]) => { sessionInput.value = id; pinInput.value = ''; startConnect(); }, [M.m2.id]);
  await k.waitForTimeout(1500);
  out.noKey = { status: await status(k) };
  out.noKey.ok = /PIN required/.test(out.noKey.status);

  const p = await page();
  fs.writeFileSync('/p/owner-id', await p.evaluate(() => ownDeviceId()));
  while (!fs.existsSync('/p/owner-ok')) await p.waitForTimeout(100);
  // Met as an owner once: m1 and m3 pin their machine's key here.
  for (const m of ['m1', 'm3']) { const r = await open(p, M[m], ''); if (!r.owner) throw new Error(m + ' did not open as an owner: ' + JSON.stringify(r)); }

  out.notOwned = await open(p, M.m2, M.m2.pin);
  out.notOwned.ok = out.notOwned.ms >= 0 && out.notOwned.ms < 1000 && out.notOwned.sent === 'own_init,pake_init' && !out.notOwned.owner;
  out.owned = await open(p, M.m1, M.m1.pin);
  out.owned.ok = out.owned.ms >= 0 && out.owned.owner && out.owned.sent.startsWith('own_init') && !out.owned.sent.includes('pake_init');
  fs.writeFileSync('/p/revoke-m3', '');
  while (!fs.existsSync('/p/revoked')) await p.waitForTimeout(100);
  out.revoked = await open(p, M.m3, M.m3.pin);
  // The host panel as drawn: no "You own this machine" for a machine that
  // dropped this device (an owner-only control would follow it).
  out.revoked.saysOwned = await p.evaluate(() => { try { renderHost(); } catch (_) {} return document.body.innerHTML.includes('You own this machine'); });
  out.revoked.ok = out.revoked.ms >= 0 && out.revoked.ms < 1500 && out.revoked.sent === 'own_init,pake_init' && !out.revoked.owner && !out.revoked.saysOwned;

  await p.evaluate(() => closeAllWarm());
  const t0 = Date.now();
  await p.evaluate((id) => connectOwned(id), M.m2.id);
  let said = -1;
  while (Date.now() - t0 < 15000) { if (/Enter the PIN/.test(await status(p))) { said = Date.now() - t0; break; } await p.waitForTimeout(50); }
  out.noPin = { saidMs: said, status: await status(p) };
  out.noPin.ok = said >= 0 && said < 6000;

  console.log(JSON.stringify(out, null, 1));
  console.log(Object.values(out).every(x => x.ok) ? 'ALL OK' : 'FAILED');
  await b.close();
  process.exit(Object.values(out).every(x => x.ok) ? 0 : 1);
})();
