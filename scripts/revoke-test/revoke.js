// Owner revocation end to end. Pages: A (owner D1), B (owner D2), D (owner D3), E (owner D4), C (PIN only).
const { chromium } = require('playwright');
const fs = require('fs');
const wait = (f) => new Promise(r => { const t = setInterval(() => { if (fs.existsSync(f)) { clearInterval(t); r(); } }, 200); });
(async () => {
  const [S] = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u, id: x.searchParams.get('s'), pin: x.hash.replace('#p=', '') }; });
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const page = async (tag, ownerOnly) => {
    const ctx = await b.newContext(); const p = await ctx.newPage();
    p.on('dialog', d => d.accept());
    p.__online = 0;
    p.on('websocket', ws => ws.on('framereceived', f => { try { if (JSON.parse(f.payload).type === 'agent_online') p.__online++; } catch {} }));
    p.tag = tag;
    if (ownerOnly) { await p.goto(S.url.replace(/[?#].*/, '')); return p; } // never given the PIN
    await p.goto(S.url);
    await p.waitForFunction(() => agentLive && (document.querySelector('.xterm-rows')?.innerText || '').includes('PIPE-OK'), null, { timeout: 20000 });
    return p;
  };
  const enroll = async (p) => { const id = await p.evaluate(() => ownDeviceId()); fs.writeFileSync('/p/enroll-' + p.tag, id); await wait('/p/enrolled-' + p.tag); p.dev = id; };
  const asOwner = async (p) => { await p.evaluate((id) => connectOwned(id), S.id); await p.waitForFunction(() => agentLive && iOwnThisMachine && cryptoKey, null, { timeout: 20000 }); };
  const state = (p) => p.evaluate(() => ({ live: agentLive, key: !!cryptoKey, own: iOwnThisMachine, status: document.getElementById('status')?.textContent }));
  const R = {};
  const A = await page('A'), B = await page('B'), D = await page('D'), E = await page('E');
  for (const p of [A, B, D, E]) { await enroll(p); await asOwner(p); }
  const C = await page('C'); // PIN only, never enrolled
  const ticks = () => fs.readFileSync('/p/ticks', 'utf8').trim();
  // The revoked-to-be device has the PIN, as its Share menu would fetch it.
  await A.evaluate(() => sendWin('host_info', { pin: true }));
  await A.waitForFunction(() => sharePin(), null, { timeout: 10000 });
  const learnedPIN = await A.evaluate(() => sharePin());
  R.ownerLearnedPIN = learnedPIN === S.pin;
  // A device that only ever connected as an owner: Share gets the PIN, nothing keeps it.
  const F = await page('F', true); await enroll(F); await asOwner(F);
  await F.evaluate(() => sendWin('host_info', { pin: true }));
  await F.waitForFunction(() => sharePin(), null, { timeout: 10000 });
  await F.evaluate(() => sendWin('host_info'));
  await F.waitForTimeout(1500);
  R.ownerStoredNoPIN = await F.evaluate((id) => !(loadHistory().find(e => e.id === id) || {}).pin && !pinValue && !JSON.stringify(localStorage).includes(String(sharePin() || 'x')), S.id);
  // 1. revoke D1 (page A)
  const c0 = C.__online;
  fs.writeFileSync('/p/revoke', A.dev); await wait('/p/revoked');
  await A.waitForFunction(() => !cryptoKey || !agentLive, null, { timeout: 20000 }).then(() => R.revokedLiveCut = true, () => R.revokedLiveCut = false);
  await A.waitForTimeout(4000);
  R.revokedState = await state(A);
  // Mid-cut, it asks for the PIN as Share would: nothing comes, old or new.
  R.revokedSharePin = await A.evaluate(async () => { sharePinFromAgent = null; try { sendWin('host_info', { pin: true }); } catch (_) {} await new Promise(r => setTimeout(r, 5000)); return sharePin() || ''; });
  // The PIN changed with the key: the old one no longer opens it.
  await C.waitForFunction(() => /PIN mismatch|failed/i.test(document.getElementById('status')?.textContent || ''), null, { timeout: 20000 })
    .then(() => R.oldPinRefused = true, () => R.oldPinRefused = false);
  const newPIN = (fs.readFileSync('/p/revoke.log', 'utf8').match(/new PIN (\d{6,8})/) || [])[1];
  R.newPinPrinted = !!newPIN && newPIN !== S.pin;
  await C.evaluate(([id, pin]) => connectToSession(id, pin), [S.id, newPIN]);
  await C.waitForFunction(() => agentLive && cryptoKey, null, { timeout: 20000 }).then(() => R.newPinWorks = true, () => R.newPinWorks = false);
  await B.waitForFunction(() => agentLive && cryptoKey && iOwnThisMachine, null, { timeout: 20000 }).then(() => R.enrolledOwnerBack = true, () => R.enrolledOwnerBack = false);
  // a fresh owner dial from the revoked device fails
  await A.evaluate((id) => connectOwned(id), S.id); await A.waitForTimeout(6000);
  R.revokedFreshDial = await state(A);
  // and so does a dial with the PIN it had learned
  await A.evaluate(([id, pin]) => connectToSession(id, pin), [S.id, learnedPIN]);
  await A.waitForFunction(() => /PIN mismatch/i.test(document.getElementById('status')?.textContent || '') || cryptoKey, null, { timeout: 25000 }).catch(() => {});
  R.revokedPinDial = await state(A);
  // 2. two devices in one command: the session restarts once
  const c1 = C.__online; fs.writeFileSync('/p/revoke.log', '');
  fs.writeFileSync('/p/revoke', B.dev + ' ' + D.dev); await wait('/p/revoked2');
  await C.waitForTimeout(8000);
  R.twoRevokesRestarts = (fs.readFileSync('/p/revoke.log', 'utf8').match(/new PIN \d+/g) || []).length;
  R.twoCut = [await state(B), await state(D)].map(s => !(s.live && s.key));
  // 3. self-revoke from page E (the Machines panel's ✕)
  const pins = () => new Set(fs.readFileSync('/p/pins', 'utf8').trim().split('\n')).size;
  const p2 = pins();
  R.selfRevokeOK = await E.evaluate(async () => { const m = await loadOwnedMachines(); const k = Object.keys(m)[0]; return k ? tryRevokeSelf(k) : 'no machine'; });
  await E.waitForTimeout(8000);
  R.selfRevokedState = await state(E);
  R.selfRevokeRestarts = pins() - p2; // a new PIN: one restart
  fs.writeFileSync('/p/done', '');
  console.log(JSON.stringify(R));
  await b.close();
})();
