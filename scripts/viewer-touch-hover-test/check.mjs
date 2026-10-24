// run.sh: buttons on a touch screen after a tap. A tap leaves :hover on the
// tapped control until something else is tapped, so:
//   touch  at 412 (hasTouch, isMobile): the viewer is walked with real taps
//          (start page, connected with the renew bar, machines, alerts,
//          windows, apps, host, what's new, settings, snippets, session dock,
//          find, share, the key bar), and in
//          each view every visible button, summary and role=button/switch/
//          tab/menuitem is checked with :hover, :active and :focus forced in
//          turn (as a tap leaves them): none turns primary green unless it is a primary
//          button, and none loses or tiles its background icon. A planted
//          control that opts out of the green with one class checks the
//          check itself.
//   mouse  at 1280: every visible control in the same views is hovered with
//          the mouse; its look (colour, and a hash of its screenshot) is
//          printed as DESK lines, to compare one tree against another.
// Screenshots go to /shots when it is mounted. Prints ALL OK or FAILED.
import { chromium } from 'playwright';
import fs from 'node:fs';
import crypto from 'node:crypto';
const [id, pin] = fs.readFileSync('/p/join.txt', 'utf8').trim().split(' ');
const shots = fs.existsSync('/shots');
const PRIMARY = ['rgb(35, 134, 54)', 'rgb(46, 160, 67)'];
let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const b = await chromium.launch();

async function open(opts) {
  const ctx = await b.newContext(opts);
  await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
  const p = await ctx.newPage();
  await p.goto('http://localhost:8080/');
  await p.waitForFunction(() => typeof connectToSession === 'function');
  // No transitions: a look is read once it is what it will be.
  await p.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' });
  p.cdp = await ctx.newCDPSession(p);
  await p.cdp.send('DOM.enable');
  await p.cdp.send('CSS.enable');
  // The check's own control: a button that opts out of the green with one
  // class, as the viewer's do. Off to the side, out of every view's way.
  await p.evaluate(() => {
    const s = document.createElement('style');
    s.textContent = '.th-probe { background: transparent; color: #8b949e; position: fixed; right: 0; bottom: 0; }';
    document.head.appendChild(s);
    const x = document.createElement('button');
    x.className = 'th-probe'; x.textContent = 'probe';
    document.body.appendChild(x);
  });
  return p;
}

// Every visible control in the page now, numbered.
async function controls(p) {
  return p.evaluate(() => {
    let i = 0;
    const out = [];
    for (const e of document.querySelectorAll('button, summary, [role=button], [role=switch], [role=tab], [role=menuitem]')) {
      const r = e.getBoundingClientRect();
      delete e.dataset.thI;
      if (!r.width || !r.height || getComputedStyle(e).visibility === 'hidden' || r.bottom < 0 || r.top > innerHeight) continue;
      e.dataset.thI = String(i);
      const who = (e.id ? '#' + e.id : '') + (e.className ? '.' + String(e.className).trim().split(/\s+/).join('.') : '') + ' "' + (e.textContent || e.title || e.getAttribute('aria-label') || '').trim().replace(/\s+/g, ' ').slice(0, 24) + '"';
      out.push({ i: i++, who });
    }
    return out;
  });
}
const look = (p, i) => p.evaluate((i) => {
  const s = getComputedStyle(document.querySelector(`[data-th-i="${i}"]`));
  return { bg: s.backgroundColor, img: s.backgroundImage, rep: s.backgroundRepeat, size: s.backgroundSize, pos: s.backgroundPosition };
}, i);

// Touch: every control with :hover forced, as a tap leaves it.
async function touchCheck(p, view) {
  const { root } = await p.cdp.send('DOM.getDocument', { depth: -1 });
  let bad = 0;
  const list = await controls(p);
  for (const c of list) for (const state of ['hover', 'active', 'focus']) {
    const { nodeId } = await p.cdp.send('DOM.querySelector', { nodeId: root.nodeId, selector: `[data-th-i="${c.i}"]` });
    if (!nodeId) continue;
    const before = await look(p, c.i);
    await p.cdp.send('CSS.forcePseudoState', { nodeId, forcedPseudoClasses: [state] });
    const after = await look(p, c.i);
    await p.cdp.send('CSS.forcePseudoState', { nodeId, forcedPseudoClasses: [] });
    const why = [];
    if (PRIMARY.includes(after.bg) && !PRIMARY.includes(before.bg)) why.push(`turns primary green (${before.bg} -> ${after.bg})`);
    if (before.img !== 'none' && after.img === 'none') why.push('loses its icon');
    if (after.img !== 'none' && (after.rep !== before.rep || after.size !== before.size || after.pos !== before.pos)) why.push(`its icon is laid out again (${before.rep} ${before.size} -> ${after.rep} ${after.size})`);
    if (why.length) { fail(`touch ${view}: after a tap (:${state}) ${c.who} ${why.join('; ')}`); bad++; }
  }
  if (!bad) ok(`touch ${view}: none of ${list.length} controls changes to primary or loses its icon after a tap`);
  if (shots) await p.screenshot({ path: `/shots/touch-${view.replace(/\W+/g, '-')}.png` });
}

// Mouse: every control hovered, its look printed to compare trees by.
async function mouseLooks(p, view) {
  const list = await controls(p);
  for (const c of list) {
    const el = await p.$(`[data-th-i="${c.i}"]`);
    if (!el) continue;
    try { await el.hover({ timeout: 2000 }); } catch (_) { continue; }
    await p.waitForTimeout(350); // past any hover transition
    const l = await look(p, c.i);
    let h = '-';
    try {
      const png = await el.screenshot({ timeout: 2000 });
      h = crypto.createHash('sha1').update(png).digest('hex').slice(0, 12);
      if (shots) fs.writeFileSync(`/shots/hover-${view.replace(/\W+/g, '-')}-${c.i}.png`, png);
    } catch (_) {}
    console.log(`DESK ${view} | ${c.who} | ${l.bg} | ${l.img === 'none' ? 'no-icon' : l.rep + ' ' + l.size} | ${h}`);
  }
  await p.mouse.move(1, 1);
  if (shots) await p.screenshot({ path: `/shots/mouse-${view.replace(/\W+/g, '-')}.png` });
}

// The views, reached the way a person would: the top bar's panels, the key
// bar's buttons, a panel's own buttons. Where a control is not on screen at
// this width (the key bar on a desktop), the viewer's own opener does what
// tapping it would.
async function walk(p, each, act0) {
  const shown = (sel) => p.evaluate((s) => { const e = document.querySelector(s); if (!e) return false; const r = e.getBoundingClientRect(); return r.width > 0 && r.height > 0 && getComputedStyle(e).visibility !== 'hidden'; }, sel);
  const act = async (sel, opener) => {
    if (await shown(sel) && await act0(sel)) return;
    if (opener) await p.evaluate(opener);
  };
  const settle = (ms = 500) => p.waitForTimeout(ms);
  await each('start page');
  await p.evaluate(([id, pin]) => connectToSession(id, pin), [id, pin]);
  await p.waitForFunction(() => agentLive && !!cryptoKey, null, { timeout: 20000 });
  await settle(1500);
  await each('connected');
  // The renew bar ("Renew alerts on this phone") shows on a phone whose
  // alerts lapsed; shown here as it would be.
  await p.evaluate(() => { const r = document.getElementById('alerts-renew'); if (r) { r.hidden = false; r.querySelector('.alr-text').textContent = 'Alerts on this phone stopped.'; } });
  await act('#recents > summary'); await settle(); await each('machines');
  await act('#alerts-bar-btn', () => openAlerts()); await settle(700); await each('alerts');
  await p.evaluate(() => { document.getElementById('alback')?.click(); document.getElementById('alerts-renew').hidden = true; }); await settle(300);
  await act('#recents > summary'); await settle(300);
  for (const [panel, name] of [['#windows', 'windows'], ['#apps', 'apps']]) {
    await act(panel + ' > summary'); await settle(700); await each(name);
    await act(panel + ' > summary'); await settle(300);
  }
  await act('#host > summary'); await settle(700); await each('host');
  await act('.host-whatsnew:not(.host-tour)', () => openChangelog()); await settle(800); await each("what's new");
  await act('#clx', () => closeChangelog()); await settle(300);
  await act('#host > summary'); await settle(300);
  await act('#settings-btn', () => document.getElementById('settings-btn').click()); await settle(); await each('settings');
  await act('#settings-btn', () => document.getElementById('settings-btn').click()); await settle(300);
  await act('#snip-btn', () => openSnipdock()); await settle(); await each('snippets');
  await act('#snip-btn', () => document.getElementById('snip-btn').click()); await settle(300);
  await p.evaluate(() => openSessdock()); await settle(); await each('session dock');
  await p.keyboard.press('Escape'); await act('#terminal'); await settle(300);
  await act('#find-btn', () => openFind()); await settle(400); await each('find');
  await act('#find-close'); await settle(300);
  await act('#share-btn', () => openShare()); await settle(); await each('share');
  await act('#share-close'); await settle(300);
}

// Touch, at phone width, with real taps.
{
  const p = await open({ viewport: { width: 412, height: 915 }, isMobile: true, hasTouch: true, deviceScaleFactor: 2.6 });
  // A tap that does not land (something laid over it a moment) is said, and
  // the walk goes on with the viewer's opener: the looks are what is checked.
  const tap = async (sel) => { const e = await p.$(sel); if (!e) return false; try { await e.scrollIntoViewIfNeeded({ timeout: 3000 }); await e.tap({ timeout: 5000 }); return true; } catch (_) { console.log(`note touch: ${sel} could not be tapped`); return false; } };
  await walk(p, (v) => touchCheck(p, v), tap);
  const kb = await p.evaluate(() => getComputedStyle(document.getElementById('keybar')).display);
  if (kb !== 'none') await touchCheck(p, 'key bar');
  await p.context().close();
}

// Mouse, at 1280: the look of every control under the pointer.
{
  const p = await open({ viewport: { width: 1280, height: 800 } });
  const click = async (sel) => { const e = await p.$(sel); if (!e) return false; try { await e.scrollIntoViewIfNeeded({ timeout: 3000 }); await e.click({ timeout: 5000 }); return true; } catch (_) { console.log(`note mouse: ${sel} could not be clicked`); return false; } };
  await walk(p, (v) => mouseLooks(p, v), click);
  await p.context().close();
}

await b.close();
console.log(fails ? 'FAILED' : 'ALL OK');
process.exit(fails ? 1 : 0);
