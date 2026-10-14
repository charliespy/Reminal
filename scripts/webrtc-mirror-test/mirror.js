// Window mirror over WebRTC: frames on the DataChannel, typing on the input channel.
const { chromium } = require('playwright');
const fs = require('fs');
(async () => {
  const [S] = fs.readFileSync(process.argv[2], 'utf8').trim().split('\n').map(u => { const x = new URL(u); return { url: u }; });
  const secs = +(process.argv[3] || 30);
  const b = await chromium.launch({ args: ['--no-sandbox'] });
  const p = await b.newPage();
  const R = {};
  await p.goto(S.url);
  await p.waitForFunction(() => agentLive && cryptoKey, null, { timeout: 20000 });
  if (process.argv[4] === 'nopane') {
    R.p2p = await p.waitForFunction(() => rtcActive(), null, { timeout: 30000 }).then(() => true, () => false);
    fs.writeFileSync('/p/mirror-phase', 'watching');
    await p.waitForTimeout(secs * 1000);
    console.log(JSON.stringify(R)); fs.writeFileSync('/p/mirror-phase', 'done'); await b.close(); return;
  }
  const list = await p.evaluate(() => nextWindowList());
  const wins = (list && (list.windows || list)) || [];
  const w = (Array.isArray(wins) ? wins : []).find(x => /xterm/i.test((x.title || '') + (x.app || '') + (x.class || '')));
  R.windowFound = !!w;
  if (!w) { console.log(JSON.stringify({ R, list }).slice(0, 600)); await b.close(); return; }
  await p.evaluate((w) => openWinView(w), w);
  R.frames = await p.waitForFunction((id) => { const x = panes.get(id); return x && x.gotFrame; }, w.id, { timeout: 30000 }).then(() => true, () => false);
  R.p2p = await p.waitForFunction(() => rtcActive(), null, { timeout: 30000 }).then(() => true, () => false);
  await p.waitForTimeout(3000);
  R.fps = await p.evaluate((id) => (panes.get(id).frameTimes || []).length, w.id);
  R.inputChannelOpen = await p.evaluate(() => !!(rtcInput && rtcInput.readyState === 'open'));
  await p.evaluate((id) => sendWin('window_input', { id, kind: 'key', text: 'echo MIRROR-OK > /tmp/typed\n' }), w.id);
  fs.writeFileSync('/p/mirror-phase', 'watching');
  await p.waitForTimeout(secs * 1000);
  console.log(JSON.stringify(R));
  fs.writeFileSync('/p/mirror-phase', 'done');
  await b.close();
})();
