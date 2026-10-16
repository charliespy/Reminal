// Drives code-server (127.0.0.1:8443) and the relay's web viewer
// (127.0.0.1:18080) from inside the test container's network. Phase "works"
// checks the three entry points; phase "missing" runs after reminal has been
// moved away and checks the error path.
import { chromium } from 'playwright';
const CODE = 'http://127.0.0.1:8443/?folder=/home/coder/demo-project';
const SHOTS = '/t/out';
const phases = process.argv.slice(2);
// Join links of the three reminal terminals the works phase opens, in tab order.
const links = [];
let phase = phases[0];
const out = [];
let fail = false;
const check = (name, ok, extra = '') => {
  out.push(`${ok ? 'PASS' : 'FAIL'}  ${name}${extra ? '  ' + extra : ''}`);
  if (!ok) fail = true;
};

const b = await chromium.launch();
const ctx = await b.newContext({
  viewport: { width: 1400, height: 860 },
  permissions: ['clipboard-read', 'clipboard-write'],
});
const p = await ctx.newPage();
await p.goto(CODE, { waitUntil: 'domcontentloaded' });
await p.waitForSelector('.monaco-workbench', { timeout: 60000 });
await p.waitForTimeout(4000);

const notifications = () => p.locator('.notification-toast');
const clearNotifications = async () => {
  await p.keyboard.press('Escape');
  await runCommand('Notifications: Clear All Notifications');
};
async function runCommand(name) {
  await p.keyboard.press('Control+Shift+P');
  await p.locator('.quick-input-widget .quick-input-box input').first().waitFor({ state: 'visible' });
  await p.waitForTimeout(300);
  await p.keyboard.type(name);
  await p.waitForTimeout(600);
  await p.keyboard.press('Enter');
  await p.waitForTimeout(800);
}
// Runs the palette entry whose label is exactly `name`, not the fuzzy top hit.
// Picks a context-menu entry: hovered and Enter, which this menu takes when a
// synthetic click on it only highlights it.
async function menuPick(name) {
  const item = p.getByRole('menuitem', { name }).first();
  await item.hover({ timeout: 8000 });
  await p.waitForTimeout(300);
  await p.keyboard.press('Enter');
  await p.waitForTimeout(1200);
}
async function runExact(name) {
  await p.keyboard.press('Control+Shift+P');
  await p.locator('.quick-input-widget .quick-input-box input').first().waitFor({ state: 'visible' });
  await p.waitForTimeout(300);
  await p.keyboard.type(name);
  await p.waitForTimeout(800);
  const row = p.locator('.quick-input-list .monaco-list-row').filter({ has: p.locator('.label-name', { hasText: new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`) }) }).first();
  await row.click({ timeout: 5000 });
  await p.waitForTimeout(1000);
}
async function waitToast(re, timeout = 30000) {
  const t = notifications().filter({ hasText: re }).first();
  await t.waitFor({ state: 'visible', timeout });
  return t;
}
// The cloud button: its list of reminal terminals, ending in a New row.
const cloud = () => p.locator('.action-item a[aria-label="reminal Terminals"]:visible').first();
const listRows = () => p.locator('.quick-input-widget .monaco-list-row:visible');
async function openList() {
  await cloud().click();
  await listRows().first().waitFor({ state: 'visible', timeout: 8000 });
  await p.waitForTimeout(400);
}
async function newViaButton() {
  await openList();
  await listRows().filter({ hasText: 'New reminal Terminal Here' }).first().click();
}
// A file or folder in the Explorer, by name.
const explorerRow = (name) => p.locator('.explorer-folders-view .monaco-list-row')
  .filter({ has: p.locator('.label-name', { hasText: new RegExp(`^${name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}$`) }) }).first();
// The terminal panel's tab list, one entry per terminal.
const tabs = () => p.locator('.terminal-tabs-entry:visible');
// A tab's text as drawn: decoration badges are CSS ::after content, which
// textContent leaves out.
const DRAWN = (sel) => [...document.querySelectorAll(sel)].map((el) => {
  let t = '';
  const walk = (n) => {
    if (n.nodeType === 3) { t += n.textContent; return; }
    if (n.nodeType !== 1) return;
    n.childNodes.forEach(walk);
    const a = getComputedStyle(n, '::after').content;
    if (a && a !== 'none' && a !== 'normal') t += ' ' + a.replace(/^"|"$/g, '');
  };
  walk(el);
  return t.replace(/\s+/g, ' ').trim();
});
// Types into the newest terminal (the last tab), the one just opened.
async function typeInTerminal(text) {
  await tabs().last().click();
  await p.waitForTimeout(400);
  // .terminal-wrapper.active on older releases; newer ones dropped the class.
  await p.locator('.terminal-wrapper.active .xterm').first().click({ timeout: 3000 })
    .catch(() => p.locator('.terminal-xterm-host .xterm:visible, .integrated-terminal .xterm-screen:visible').last().click());
  await p.keyboard.type(text);
  await p.keyboard.press('Enter');
}
// A close-up of the terminal panel's tab list.
async function tabShot(name) {
  const box = await p.locator('.terminal-tabs-entry').first().locator('xpath=ancestor::div[contains(@class,"tabs-container")]').first().boundingBox().catch(() => null);
  const clip = box ? { x: box.x - 420, y: box.y - 40, width: box.width + 440, height: Math.min(box.height + 60, 200) } : undefined;
  await p.screenshot({ path: `${SHOTS}/${name}`, clip });
}
// Copy link from a reminal toast, then read the link back off the clipboard.
async function copyLink(toast) {
  await toast.getByRole('button', { name: 'Copy link' }).click();
  await p.waitForTimeout(500);
  return p.evaluate(() => navigator.clipboard.readText());
}
// Opens a join link in a fresh viewer and waits for text to show up in it.
async function viewerShows(url, re, shot) {
  const v = await ctx.newPage();
  await v.goto(url, { waitUntil: 'domcontentloaded' });
  // The first-visit tour would cover the screenshot.
  await v.getByRole('button', { name: 'Skip' }).click({ timeout: 8000 }).catch(() => {});
  const ok = await v.waitForFunction((src) => {
    const r = document.querySelector('.xterm-rows');
    return r && new RegExp(src).test(r.innerText);
  }, re.source, { timeout: 30000 }).then(() => true, () => false);
  await v.screenshot({ path: `${SHOTS}/${shot}` });
  await v.close();
  return ok;
}

for (phase of phases) {
try {
if (phase === 'works') {
  // A plain bash terminal, moved into a subfolder: that's the folder the
  // button and the menu item should carry over.
  await runCommand('Terminal: Create New Terminal');
  await p.waitForSelector('.terminal-wrapper.active .xterm', { timeout: 20000 });
  await p.waitForTimeout(1500);
  await p.locator('.terminal-wrapper.active .xterm').first().click();
  await p.keyboard.type('cd src && clear');
  await p.keyboard.press('Enter');
  await p.waitForTimeout(1000);

  // ---- 1. the terminal profile ----
  await p.locator('[aria-label^="Launch Profile"]').first().click();
  await p.waitForTimeout(800);
  await p.screenshot({ path: `${SHOTS}/1-profile-dropdown.png` });
  await p.getByRole('menuitem', { name: /^reminal$/ }).or(p.locator('.action-label', { hasText: /^reminal$/ })).first().click();
  let toast = await waitToast(/reminal is sharing/);
  await p.waitForTimeout(1500);
  await p.screenshot({ path: `${SHOTS}/2-profile-notification.png` });
  check('profile opens a reminal session and offers its link', true, (await toast.innerText()).replace(/\s+/g, ' '));
  const link1 = await copyLink(toast);
  links.push(link1);
  check('Copy link puts the join link on the clipboard', /^http:\/\/127\.0\.0\.1:18080\/.*#p=\d{6}$/.test(link1), link1);
  await typeInTerminal('echo vsce-profile-ok');
  check('web viewer reaches the profile session', await viewerShows(link1, /vsce-profile-ok/, '3-viewer-profile.png'));

  // ---- 2. the title-bar button, with the bash terminal (in src/) active ----
  await clearNotifications();
  await tabs().filter({ hasText: 'bash' }).first().click();
  await p.waitForTimeout(500);
  await openList();
  await p.screenshot({ path: `${SHOTS}/4a-cloud-list-one.png` });
  const firstRows = (await listRows().allInnerTexts()).map((r) => r.trim()).filter(Boolean);
  check('cloud button lists the reminal terminal, then New reminal Terminal Here and Share a Port',
    firstRows.length === 3 && /demo-project/.test(firstRows[0]) && /New reminal Terminal Here/.test(firstRows[1]) && /Share a Port/.test(firstRows[2]), JSON.stringify(firstRows));
  await listRows().filter({ hasText: 'New reminal Terminal Here' }).first().click();
  toast = await waitToast(/reminal is sharing/);
  const t2 = (await toast.innerText()).replace(/\s+/g, ' ');
  await p.screenshot({ path: `${SHOTS}/4-button-notification.png` });
  check('title-bar button opens reminal in the active terminal\'s folder', /demo-project\/src/.test(t2), t2);
  const link2 = await copyLink(toast);
  links.push(link2);
  await typeInTerminal('pwd');
  check('web viewer reaches the button session, in src/', await viewerShows(link2, /demo-project\/src/, '5-viewer-button.png'));

  // ---- 3. Open reminal in This Folder, from the bash tab's right-click menu ----
  await clearNotifications();
  const before = await tabs().count();
  await tabs().filter({ hasText: 'bash' }).first().click({ button: 'right' });
  await p.waitForTimeout(600);
  await p.screenshot({ path: `${SHOTS}/6-context-menu.png` });
  await p.getByRole('menuitem', { name: 'Open reminal in This Folder' }).click();
  const ask = await waitToast(/Close "bash" too\?/);
  toast = await waitToast(/reminal is sharing/);
  const t3 = (await toast.innerText()).replace(/\s+/g, ' ');
  await p.screenshot({ path: `${SHOTS}/7-continue-notifications.png` });
  check('Open reminal in This Folder opens reminal in that terminal\'s folder', /demo-project\/src/.test(t3), t3);
  await ask.getByRole('button', { name: 'Close it' }).click();
  await p.waitForTimeout(1500);
  const names = await tabs().allInnerTexts();
  check('"Close it" closes the original terminal', !names.some((n) => /bash/.test(n)) && names.length === before,
    JSON.stringify(names.map((n) => n.replace(/\s+/g, ' ').trim())));
  const link3 = await copyLink(toast);
  links.push(link3);
  await typeInTerminal('echo vsce-continue-ok');
  check('web viewer reaches the continued session', await viewerShows(link3, /vsce-continue-ok/, '8-viewer-continue.png'));
  await p.screenshot({ path: `${SHOTS}/9-three-reminal-terminals.png` });
}

if (phase === 'status') {
  // A fake agent in the newest reminal terminal reports working, then needs
  // you, then done, through the same `reminal hook` an integration fires.
  await runCommand('Terminal: Focus Terminal');
  await p.waitForSelector('.terminal-wrapper.active .xterm', { timeout: 20000 });
  await clearNotifications();
  const bar = p.locator('.statusbar-item').filter({ hasText: /reminal:/ }).first();
  const barSays = async (re, timeout = 15000) => {
    const ok = await p.waitForFunction(([sel, src]) => [...document.querySelectorAll(sel)]
      .some((e) => new RegExp(src).test(e.textContent || '')), ['.statusbar-item', re.source], { timeout })
      .then(() => true, () => false);
    return [ok, (await bar.innerText().catch(() => '')).trim()];
  };
  // The newest tab is where the fake agent runs; its name carries the state,
  // and no other tab's does.
  const tabSays = async (re, timeout = 15000) => {
    const ok = await p.waitForFunction(([drawn, src]) => {
      const t = (0, eval)(drawn)('.terminal-tabs-entry');
      return t.length > 1 && new RegExp(src).test(t[t.length - 1]) && t.slice(0, -1).every((x) => !/[\u2026!\u2713]/.test(x));
    }, [DRAWN.toString(), re.source], { timeout }).then(() => true, () => false);
    return [ok, JSON.stringify(await p.evaluate((drawn) => (0, eval)(drawn)('.terminal-tabs-entry'), DRAWN.toString()))];
  };
  // The cloud button's list: the newest terminal's row (the last before New
  // and Share a Port) carries the same state as its tab.
  const listSays = async (state, shot) => {
    await openList();
    await p.screenshot({ path: `${SHOTS}/${shot}` });
    const rows = (await listRows().allInnerTexts()).map((r) => r.trim()).filter(Boolean);
    await p.keyboard.press('Escape');
    await p.waitForTimeout(300);
    return [rows.length === 5 && rows[2].includes(state) && !rows[0].includes(state) && !rows[1].includes(state), JSON.stringify(rows)];
  };
  let [ok, text] = await barSays(/reminal: 3$/);
  await p.screenshot({ path: `${SHOTS}/12-status-idle.png` });
  check('status bar counts the reminal terminals before any agent runs', ok, text);

  await typeInTerminal('clear; fake-agent');
  [ok, text] = await barSays(/reminal: 1 working$/);
  await p.screenshot({ path: `${SHOTS}/13-status-working.png` });
  check('status bar shows Working', ok, text);
  [ok, text] = await tabSays(/reminal.*\u2026/);
  await tabShot('13b-tab-working.png');
  check('the tab shows the Working badge beside its name', ok, text);
  [ok, text] = await listSays('Working', '13c-list-working.png');
  check('the cloud button list shows Working on that terminal\'s row', ok, text);

  // Look at another terminal, so the nudge has someone to nudge.
  await tabs().first().click();
  [ok, text] = await barSays(/reminal: 1 needs you$/);
  const nudge = await waitToast(/needs you/, 10000).then(() => true, () => false);
  await p.waitForTimeout(500);
  await p.screenshot({ path: `${SHOTS}/14-status-needs-you.png` });
  check('status bar shows Needs you', ok, text);
  [ok, text] = await tabSays(/reminal.*!/);
  await tabShot('14b-tab-needs-you.png');
  check('the tab shows the Needs you badge beside its name', ok, text);
  check('a notification says which terminal needs you', nudge);
  [ok, text] = await listSays('Needs you', '14c-list-needs-you.png');
  check('the cloud button list shows Needs you on that terminal\'s row', ok, text);
  await bar.hover();
  await p.waitForTimeout(1200);
  await p.screenshot({ path: `${SHOTS}/15-status-tooltip.png` });

  // Clicking the item must land on the waiting terminal: the Enter typed
  // there is what lets the fake agent go on, so a wrong terminal stalls it.
  await bar.click();
  await p.waitForTimeout(800);
  await p.locator('.terminal-wrapper.active .xterm').first().click();
  await p.keyboard.press('Enter');
  [ok, text] = await barSays(/reminal: 1 working$/);
  check('clicking the status bar goes to the terminal that needs you', ok, text);
  [ok, text] = await barSays(/reminal: 1 done$/, 25000);
  await p.waitForTimeout(500);
  await p.screenshot({ path: `${SHOTS}/16-status-done.png` });
  check('status bar shows Done', ok, text);
  [ok, text] = await tabSays(/reminal.*\u2713/);
  await tabShot('16b-tab-done.png');
  check('the tab shows the Done badge beside its name', ok, text);
  [ok, text] = await listSays('Done', '16c-list-done.png');
  check('the cloud button list shows Done on that terminal\'s row', ok, text);
}

if (phase === 'share') {
  // Each link is http://host/?s=ID#p=PIN.
  const cred = (l) => { const m = l.match(/[?&]s=([A-Z0-9]+)#p=(\d+)$/); return { id: m[1], pin: m[2], url: l }; };
  const want = links.map(cred);
  const clip = () => p.evaluate(() => navigator.clipboard.readText());
  const shareFrom = async (i, item, shot) => {
    await p.mouse.move(700, 300);
    await p.evaluate(() => navigator.clipboard.writeText('-'));
    await tabs().nth(i).click({ button: 'right', position: { x: 12, y: 10 } });
    await p.waitForTimeout(500);
    if (shot) await p.screenshot({ path: `${SHOTS}/${shot}` });
    await menuPick(item);
    return clip();
  };
  await clearNotifications();
  // Tabs 0 and 1 are not the active terminal (the last one is): what's
  // copied must be the right-clicked tab's session, not the active one's.
  for (const i of [0, 1]) {
    const text = await shareFrom(i, 'Share for a Person', i === 0 ? '21-share-menu.png' : undefined);
    const w = want[i];
    const expect = `Join this reminal terminal:\n\n${w.url}\n\nOr from a terminal:\nreminal connect ${w.id} ${w.pin}\n\nAnyone with these details can type in the same shell.`;
    check(`Share for a Person on tab ${i} (not active) copies that tab's session`, text === expect, JSON.stringify(text));
    if (i === 0) {
      await waitToast(/Copied a join message/, 5000).catch(() => {});
      await p.screenshot({ path: `${SHOTS}/22-share-person-copied.png` });
    }
  }
  const agent = await shareFrom(1, 'Share for an AI Agent');
  const w1 = want[1];
  check('Share for an AI Agent copies the agent text for that tab',
    agent.startsWith('You are an AI agent.') && agent.includes(`  reminal connect ${w1.id} ${w1.pin}\n`) &&
    agent.includes('press Ctrl-].') && agent.includes(`  ${w1.url}\n`) && agent.includes(`PIN: ${w1.pin}\n`),
    JSON.stringify(agent.slice(0, 80) + '…'));
  await p.screenshot({ path: `${SHOTS}/23-share-agent-copied.png` });

  // The cloud button: the same list whatever is in front. Picking a row
  // brings that terminal forward; nothing is shared from here.
  await clearNotifications();
  await tabs().last().click({ position: { x: 12, y: 10 } });
  await p.waitForTimeout(500);
  await p.evaluate(() => navigator.clipboard.writeText('-'));
  await openList();
  await p.screenshot({ path: `${SHOTS}/24-cloud-list.png` });
  const rows = (await listRows().allInnerTexts()).map((r) => r.trim()).filter(Boolean);
  const ids = want.map((w) => w.id.toUpperCase());
  check('cloud button lists every reminal terminal in tab order, with its ID, then New and Share a Port',
    rows.length === want.length + 2 && ids.every((id, i) => rows[i].includes(id)) && /New reminal Terminal Here/.test(rows[rows.length - 2]) &&
    /Share a Port/.test(rows[rows.length - 1]) && !rows.slice(0, -1).some((r) => /Share/.test(r)), JSON.stringify(rows));
  await listRows().filter({ hasText: ids[0] }).first().click();
  await p.waitForTimeout(800);
  // Typed into whatever terminal is now in front, without touching the tabs:
  // it must turn up in the first session's viewer.
  const copied = await clip();
  await p.locator('.terminal-wrapper.active .xterm').first().click({ timeout: 3000 })
    .catch(() => p.locator('.terminal-xterm-host .xterm:visible, .integrated-terminal .xterm-screen:visible').last().click());
  await p.waitForTimeout(600);
  // The first key after this click is dropped, so it is a spare space.
  await p.keyboard.type(' echo list-pick-$((6*7))');
  await p.keyboard.press('Enter');
  check('picking a row brings that terminal forward and copies nothing',
    copied === '-' && await viewerShows(links[0], /list-pick-42/, '24b-list-pick-viewer.png'), JSON.stringify(copied));
  const w2 = want[2];
  // For run.sh: the line a person would paste, to try from another terminal.
  console.log(`SHARE-CONNECT reminal connect ${w2.id} ${w2.pin}`);

  // A plain terminal: nothing to share, and it says what to do instead.
  await clearNotifications();
  await runCommand('Terminal: Create New Terminal');
  await p.waitForTimeout(2500);
  const plain = (await tabs().count()) - 1;
  await p.evaluate(() => navigator.clipboard.writeText('-'));
  await tabs().nth(plain).click({ button: 'right', position: { x: 12, y: 10 } });
  await menuPick('Share for a Person');
  const nothing = await waitToast(/isn't a reminal session/, 5000).then(() => true, () => false);
  await p.screenshot({ path: `${SHOTS}/25-share-on-plain-terminal.png` });
  check('Share on a plain terminal copies nothing and offers Open reminal in This Folder',
    nothing && (await clip()) === '-' &&
    (await notifications().filter({ hasText: /isn't a reminal session/ }).getByRole('button', { name: 'Open reminal in This Folder' }).count()) === 1);
  await clearNotifications();
  await tabs().nth(plain).click({ button: 'right', position: { x: 12, y: 10 } });
  await menuPick('Kill Terminal');
  await p.waitForTimeout(800);
}

if (phase === 'badges') {
  // The tab badges alone, with no notification UI in the way: for checking
  // them on another VS Code release (CODE_SERVER=…).
  await runCommand('Terminal: Focus Terminal');
  await p.waitForSelector('.terminal-wrapper.active .xterm', { timeout: 20000 });
  await newViaButton();
  await p.waitForTimeout(4000);
  await newViaButton();
  await p.waitForTimeout(6000);
  const drawn = () => p.evaluate((d) => (0, eval)(d)('.terminal-tabs-entry'), DRAWN.toString());
  const until = async (re, ms = 20000) => {
    const t0 = Date.now();
    let last = [];
    while (Date.now() - t0 < ms) {
      last = await drawn();
      if (last.some((t) => re.test(t))) return [true, last];
      await p.waitForTimeout(500);
    }
    return [false, last];
  };
  await typeInTerminal('clear; fake-agent');
  let [ok, d] = await until(/\u2026/);
  await tabShot('b1-tab-working.png');
  check('badge: Working', ok, JSON.stringify(d));
  [ok, d] = await until(/!/);
  await tabShot('b2-tab-needs-you.png');
  check('badge: Needs you', ok, JSON.stringify(d));
  await typeInTerminal('');
  [ok, d] = await until(/\u2713/, 30000);
  await tabShot('b3-tab-done.png');
  check('badge: Done', ok, JSON.stringify(d));
}

if (phase === 'views') {
  // The same Done badge everywhere a terminal is drawn. Hover the done tab
  // for its tooltip first.
  if (!(await tabs().count())) {
    await runCommand('Terminal: Focus Terminal');
    await p.waitForTimeout(6000);
  }
  const drawnTabs = await p.evaluate((drawn) => (0, eval)(drawn)('.terminal-tabs-entry'), DRAWN.toString());
  const doneIdx = drawnTabs.findIndex((t) => t.includes('\u2713'));
  out.push(`INFO  tabs as drawn: ${JSON.stringify(drawnTabs)}`);
  const doneTab = tabs().nth(Math.max(doneIdx, 0));
  await doneTab.hover();
  await p.waitForTimeout(1500);
  await p.screenshot({ path: `${SHOTS}/17-tab-hover.png` });

  // Narrow tab list: icons only, no badges, so the colour is all there is.
  // The list's left edge is a sash; drag it to the far right. (No Escape to
  // close the hover: it would be typed into the agent's terminal.)
  await p.mouse.move(700, 300);
  await p.waitForTimeout(800);
  const list = await p.locator('.terminal-tabs-entry').first().boundingBox();
  await p.mouse.move(list.x - 3, list.y + 60);
  await p.mouse.down();
  await p.mouse.move(list.x + 15, list.y + 60, { steps: 3 });
  await p.mouse.move(1395, list.y + 60, { steps: 12 });
  await p.mouse.up();
  await p.waitForTimeout(1000);
  const narrow = await p.evaluate((d) => (0, eval)(d)('.terminal-tabs-entry'), DRAWN.toString());
  await p.screenshot({ path: `${SHOTS}/18-narrow-tab-list.png` });
  out.push(`INFO  narrow list as drawn: ${JSON.stringify(narrow)}`);
  const wide = await p.locator('.terminal-tabs-entry').first().boundingBox();
  await p.mouse.move(wide.x - 3, wide.y + 60);
  await p.mouse.down();
  await p.mouse.move(wide.x - 20, wide.y + 60, { steps: 3 });
  await p.mouse.move(wide.x - 160, wide.y + 60, { steps: 12 });
  await p.mouse.up();
  await p.waitForTimeout(800);

  // Editor area: the done terminal as an editor tab.
  await p.mouse.move(700, 300);
  await p.waitForTimeout(500);
  // Near the left edge: the middle of the active tab is its Split button.
  await doneTab.click({ button: 'right', position: { x: 12, y: 10 } });
  await menuPick('Move Terminal into Editor Area');
  await p.waitForTimeout(1500);
  const edText = (await p.evaluate((drawn) => (0, eval)(drawn)('.editor .tabs-container .tab'), DRAWN.toString())).join(' | ');
  await p.screenshot({ path: `${SHOTS}/19-editor-area-tab.png` });
  // Whatever state reminal reports at this moment (moving the terminal
  // resizes it, and the agent's state can change with it): the editor tab's
  // badge must match the status bar's.
  const barNow = (await p.locator('.statusbar-item').filter({ hasText: /reminal:/ }).first().innerText().catch(() => '')).trim();
  const want = /needs you/.test(barNow) ? '!' : /working/.test(barNow) ? '\u2026' : '\u2713';
  check('the editor-area tab shows the badge for its state', edText.includes(want), `${JSON.stringify(edText)} vs status bar "${barNow}"`);
  await p.locator('.editor .tabs-container .tab').first().click({ button: 'right' });
  await menuPick(/Move Terminal into Panel|Move into Panel/);
  await p.waitForTimeout(1000);

  // One terminal left: the list hides and the panel header shows that one.
  for (let guard = 0; guard < 5 && (await tabs().count()) > 1; guard++) {
    const drawn = await p.evaluate((d) => (0, eval)(d)('.terminal-tabs-entry'), DRAWN.toString());
    const i = drawn.findIndex((t) => !t.includes('\u2713'));
    if (i < 0) break;
    await tabs().nth(i).click({ button: 'right', position: { x: 12, y: 10 } });
    await menuPick('Kill Terminal');
    await p.waitForTimeout(800);
  }
  await p.waitForTimeout(1000);
  const header = (await p.evaluate((d) => (0, eval)(d)('.single-terminal-tab'), DRAWN.toString())).join(' | ');
  await p.screenshot({ path: `${SHOTS}/20-single-tab-header.png` });
  // Expected to fail: this header draws its own label and asks no decoration
  // provider (terminalView.ts), so it is recorded, not required.
  out.push(`INFO  single-terminal header as drawn: ${JSON.stringify(header)}`);
}

if (phase === 'missing') {
  // A fresh browser window: bring back the panel with the terminals still
  // running from the first phase.
  await runCommand('Terminal: Focus Terminal');
  await p.waitForSelector('.terminal-wrapper.active .xterm', { timeout: 20000 });
  await p.waitForTimeout(1500);
  const before = await tabs().count();
  await clearNotifications();
  await newViaButton();
  let t = await waitToast(/reminal is not installed/, 10000);
  await p.screenshot({ path: `${SHOTS}/10-not-installed.png` });
  check('button with reminal missing explains how to install it', true, (await t.innerText()).replace(/\s+/g, ' '));
  await t.getByRole('button', { name: 'Copy install command' }).click();
  await p.waitForTimeout(500);
  const cmd = await p.evaluate(() => navigator.clipboard.readText());
  check('Copy install command copies the installer one-liner', /install\.sh \| sh$/.test(cmd), cmd);

  await clearNotifications();
  await p.locator('[aria-label^="Launch Profile"]').first().click();
  await p.waitForTimeout(800);
  await p.getByRole('menuitem', { name: /^reminal$/ }).or(p.locator('.action-label', { hasText: /^reminal$/ })).first().click();
  t = await waitToast(/reminal is not installed/, 10000);
  await p.waitForTimeout(1000);
  await p.screenshot({ path: `${SHOTS}/11-profile-not-installed.png` });
  check('profile with reminal missing explains how to install it', true);
  check('no broken terminal was opened', (await tabs().count()) === before, `${before} -> ${await tabs().count()}`);
}

if (phase === 'install') {
  await runCommand('Terminal: Focus Terminal');
  await p.waitForSelector('.terminal-wrapper.active .xterm, .terminal-xterm-host .xterm', { timeout: 20000 });
  await p.waitForTimeout(1500);
  await clearNotifications();
  await newViaButton();
  const t = await waitToast(/reminal is not installed/, 10000);
  check('the not-installed message offers Install reminal',
    (await t.getByRole('button', { name: 'Install reminal' }).count()) === 1);
  // The first install fails: that is said, and nothing claims it worked.
  await t.getByRole('button', { name: 'Install reminal' }).click();
  const failed = await waitToast(/install didn't finish/, 20000).then(() => true, () => false);
  await p.screenshot({ path: `${SHOTS}/25b-install-failed.png` });
  check('a failed install says so', failed && (await notifications().filter({ hasText: /reminal is installed/ }).count()) === 0);
  await runCommand('Terminal: Kill the Active Terminal Instance');
  await p.waitForTimeout(1500);
  await clearNotifications();
  await newViaButton();
  await (await waitToast(/reminal is not installed/, 10000)).getByRole('button', { name: 'Install reminal' }).click();
  const inst = tabs().filter({ hasText: 'Install reminal' });
  const opened = await inst.first().waitFor({ timeout: 10000 }).then(() => true, () => false);
  await p.waitForTimeout(4000);
  await p.screenshot({ path: `${SHOTS}/26-installing.png` });
  // Its text is drawn on a canvas; run.sh checks the installer ran in it.
  check('Install reminal opens a terminal of its own for the install command', opened);
  // reminal is already in place, but the installer is still asking: no
  // "installed" yet.
  const early = await waitToast(/reminal is installed/, 6000).then(() => true, () => false);
  check('it waits for the installer to finish before saying reminal is installed', !early);
  await p.locator('.terminal-xterm-host .xterm:visible, .terminal-wrapper.active .xterm').last().click();
  await p.waitForTimeout(600);
  await p.keyboard.press('Enter');
  await p.waitForTimeout(500);
  await p.keyboard.press('Enter');
  const done = await waitToast(/reminal is installed/, 15000).then((x) => x, () => null);
  await p.screenshot({ path: `${SHOTS}/27-installed.png` });
  check('once the installer finishes it says reminal is installed', !!done);
  if (done) {
    await done.getByRole('button', { name: 'New reminal Terminal Here' }).click();
    const s = await waitToast(/reminal is sharing/, 30000).then((x) => x.innerText(), () => '');
    check('New reminal Terminal Here from that message opens a reminal session', /reminal is sharing/.test(s), s.replace(/\s+/g, ' '));
  }
}

if (phase === 'revive') {
  // A window reload: a new extension host finds the terminals already
  // running, and the tab list asks about every tab before it knows them all.
  // The badge must still turn up, on the right tab and no other. Its own
  // terminals: an agent in the middle one of three.
  await runCommand('Terminal: Kill All Terminals');
  await p.waitForTimeout(2000);
  await clearNotifications();
  for (let i = 0; i < 3; i++) {
    await runExact('reminal: New reminal Terminal Here');
    await waitToast(/reminal is sharing/, 30000);
    await clearNotifications();
  }
  const drawn = () => p.evaluate((d) => (0, eval)(d)('.terminal-tabs-entry'), DRAWN.toString());
  const badged = /[\u2026!\u2713]/;
  // Done, or Needs you: growing a terminal can bring back a question the
  // agent cleared away (a reminal quirk of its own). Which state is not
  // what this is about; which tab is.
  const middleOnly = async (re, ms) => {
    const t0 = Date.now();
    let d = [];
    while (Date.now() - t0 < ms) {
      d = await drawn();
      if (d.length === 3 && re.test(d[1]) && !badged.test(d[0]) && !badged.test(d[2])) return [true, d];
      await p.waitForTimeout(500);
    }
    return [false, d];
  };
  await tabs().nth(1).click({ position: { x: 12, y: 10 } });
  await p.waitForTimeout(500);
  await p.locator('.terminal-xterm-host .xterm:visible, .terminal-wrapper.active .xterm').last().click();
  await p.waitForTimeout(600);
  await p.keyboard.type(' clear; WORK=2 fake-agent');
  await p.keyboard.press('Enter');
  await p.waitForTimeout(5000);
  await p.keyboard.type('x');
  await p.keyboard.press('Enter');
  let [ok, d] = await middleOnly(/\u2713/, 30000);
  check('before the reload, only the middle tab shows Done', ok, JSON.stringify(d));
  // Reload with the panel closed and open it once the extension has found
  // its sessions: the tabs are then drawn, and asked about, after the
  // extension last said anything changed.
  await runCommand('View: Close Panel');
  await p.reload({ waitUntil: 'domcontentloaded' });
  await p.waitForSelector('.monaco-workbench', { timeout: 60000 });
  await p.waitForTimeout(15000);
  await runCommand('Terminal: Focus Terminal');
  [ok, d] = await middleOnly(/[!\u2713]/, 30000);
  await tabShot('28-tabs-after-reload.png');
  check('after a window reload, the badge is back on that tab and no other', ok, JSON.stringify(d));

  // Stop sharing in the first terminal (Ctrl-]; its shell carries on). Its
  // old link and PIN let no one in now: the list says so, Share copies
  // nothing, and Share's "Open reminal in This Folder" opens a new reminal
  // terminal rather than offering that old link.
  await clearNotifications();
  await tabs().nth(0).click({ position: { x: 12, y: 10 } });
  await p.waitForTimeout(500);
  await p.locator('.terminal-xterm-host .xterm:visible, .terminal-wrapper.active .xterm').last().click();
  await p.waitForTimeout(600);
  await p.keyboard.type(' ');
  await p.keyboard.press('Control+BracketRight');
  // The list catches up on its next poll; how long that took is reported.
  const stoppedAt = Date.now();
  out.push(`INFO  Ctrl-] pressed at ${new Date(stoppedAt).toISOString()}`);
  let first = '';
  while (Date.now() - stoppedAt < 30000) {
    await p.waitForTimeout(2000);
    await openList();
    first = ((await listRows().first().innerText()) || '').replace(/\s+/g, ' ');
    if (/Not sharing/.test(first)) break;
    await p.keyboard.press('Escape');
  }
  await p.screenshot({ path: `${SHOTS}/29-list-not-sharing.png` });
  await p.keyboard.press('Escape');
  check('the cloud button list shows a stopped terminal as Not sharing', /Not sharing/.test(first),
    `${JSON.stringify(first)} after ${Math.round((Date.now() - stoppedAt) / 1000)}s`);
  if (!/Not sharing/.test(first)) {
    // What the window had at that moment, for the log.
    await openList();
    out.push(`INFO  list rows: ${JSON.stringify(await listRows().allInnerTexts())}`);
    await p.keyboard.press('Escape');
    out.push(`INFO  tabs: ${JSON.stringify(await p.evaluate((d) => (0, eval)(d)('.terminal-tabs-entry'), DRAWN.toString()))}`);
  }
  await p.evaluate(() => navigator.clipboard.writeText('-'));
  const before = await tabs().count();
  await tabs().nth(0).click({ button: 'right', position: { x: 12, y: 10 } });
  await menuPick('Share for a Person');
  const gone = await waitToast(/isn't sharing any more/, 8000).then((t) => t, () => undefined);
  check('Share on a stopped terminal says it isn\'t sharing and copies nothing',
    !!gone && (await p.evaluate(() => navigator.clipboard.readText())) === '-');
  if (gone) {
    await gone.getByRole('button', { name: 'Open reminal in This Folder' }).click();
    const fresh = await waitToast(/reminal is sharing/, 30000).then(() => true, () => false);
    await p.screenshot({ path: `${SHOTS}/30-stopped-open-new.png` });
    const oldLink = await notifications().filter({ hasText: /^reminal session/ }).count();
    check('its Open reminal in This Folder opens a new reminal terminal, not the old link',
      fresh && (await tabs().count()) === before + 1 && oldLink === 0, `tabs ${before} -> ${await tabs().count()}`);
  }

  // A folder named like a link, [docs](command:...): notifications read
  // [text](target) as a link, so its name must reach them as plain text.
  await clearNotifications();
  await runCommand('Terminal: Create New Terminal');
  await p.waitForTimeout(2500);
  await p.locator('.terminal-xterm-host .xterm:visible, .terminal-wrapper.active .xterm').last().click();
  await p.waitForTimeout(600);
  await p.keyboard.type(" cd '[docs](command:workbench.action.showCommands)'");
  await p.keyboard.press('Enter');
  await p.waitForTimeout(1500);
  await runExact('reminal: New reminal Terminal Here');
  const odd = await waitToast(/reminal is sharing .*docs/, 30000).then((t) => t, () => undefined);
  await p.screenshot({ path: `${SHOTS}/31-link-named-folder.png` });
  const msg = odd ? odd.locator('.notification-list-item-message') : undefined;
  const links = msg ? await msg.locator('a').count() : -1;
  const text = msg ? (await msg.innerText()).replace(/\s+/g, ' ') : '';
  check('a folder named like a link shows as plain text in the notification',
    !!odd && links === 0 && /docs\(command:/.test(text), `links ${links}: ${JSON.stringify(text)}`);
}

if (phase === 'front') {
  // A reminal terminal from the + menu comes straight to the front. VS Code
  // makes the last tab active before the profile's terminal exists, so
  // without the extension's help another terminal shows for a moment. Every
  // change of the selected tab is recorded as it happens.
  if ((await tabs().count()) < 2) await runCommand('Terminal: Create New Terminal');
  await p.waitForTimeout(1500);
  await tabs().first().click({ position: { x: 12, y: 10 } });
  await p.waitForTimeout(800);
  const n = await tabs().count();
  await p.evaluate(() => {
    window.__sel = [];
    const snap = () => {
      const i = [...document.querySelectorAll('.terminal-tabs-entry')].findIndex((e) => e.closest('.monaco-list-row')?.classList.contains('selected'));
      if (window.__sel.at(-1) !== i) window.__sel.push(i);
      requestAnimationFrame(snap);
    };
    snap();
  });
  await p.locator('[aria-label^="Launch Profile"]').first().click();
  await p.waitForTimeout(800);
  await p.getByRole('menuitem', { name: /^reminal$/ }).first().click();
  await waitToast(/reminal is sharing/);
  await p.waitForTimeout(1000);
  const seen = await p.evaluate(() => window.__sel);
  check('a reminal terminal from the + menu comes straight to the front, with no other tab shown first',
    seen[0] === 0 && seen.at(-1) === n && seen.slice(1).every((i) => i === n || i === -1), `tabs selected: ${JSON.stringify(seen)}`);
  await clearNotifications();
}

if (phase === 'cancel') {
  // A paste that hangs (run.sh points this container's relay at a listener
  // that never answers) can be stopped from its progress notification.
  await p.keyboard.press('Control+Shift+E');
  await p.waitForTimeout(1500);
  await explorerRow('src').click({ button: 'right' });
  await p.waitForTimeout(900);
  await menuPick('reminal Paste…');
  const box = p.locator('.quick-input-widget .quick-input-box input').first();
  await box.waitFor({ state: 'visible', timeout: 8000 });
  await box.fill('ABCDE-FGHJK');
  await p.keyboard.press('Enter');
  const progress = await waitToast(/Pasting into src/, 15000);
  await p.waitForTimeout(2000);
  await p.screenshot({ path: `${SHOTS}/48-paste-progress.png` });
  const cancel = progress.getByRole('button', { name: 'Cancel' });
  const hasCancel = await cancel.count() > 0;
  if (hasCancel) await cancel.first().click();
  const stopped = await waitToast(/reminal Paste was stopped\. Part of it may already be in src/, 15000).then(() => true, () => false);
  await p.screenshot({ path: `${SHOTS}/49-paste-cancelled.png` });
  check('a paste in progress can be cancelled, and says so', hasCancel && stopped, `cancel button ${hasCancel}`);
  await clearNotifications();
}

if (phase === 'transfer') {
  // reminal Copy in the Explorer's right-click menu, on a file and on a
  // folder; run.sh pastes both on another machine. reminal Paste is on
  // folders only.
  await p.keyboard.press('Control+Shift+E');
  await p.waitForTimeout(1500);
  for (const [name, kind] of [['notes.txt', 'FILE'], ['assets', 'FOLDER']]) {
    await explorerRow(name).click({ button: 'right' });
    await p.waitForTimeout(900);
    const items = (await p.getByRole('menuitem').allInnerTexts()).map((t) => t.replace(/\s+/g, ' ').trim());
    await p.screenshot({ path: `${SHOTS}/40-explorer-menu-${kind.toLowerCase()}.png` });
    const copy = items.some((t) => /^reminal Copy\b/.test(t)), paste = items.some((t) => /^reminal Paste/.test(t));
    if (kind === 'FILE') check('a file\'s right-click menu has reminal Copy and no reminal Paste', copy && !paste, JSON.stringify(items.filter((t) => /reminal/.test(t))));
    else check('a folder\'s right-click menu has reminal Copy and reminal Paste', copy && paste, JSON.stringify(items.filter((t) => /reminal/.test(t))));
    await menuPick('reminal Copy');
    const t = await waitToast(/is ready to paste with code/, 120000);
    await p.screenshot({ path: `${SHOTS}/41-copied-${kind.toLowerCase()}.png` });
    const code = /code ([A-Z0-9]{5}-[A-Z0-9]{5})/.exec(await t.innerText())?.[1];
    await t.getByRole('button', { name: kind === 'FILE' ? 'Copy code' : 'Copy paste command' }).click();
    await p.waitForTimeout(500);
    const clip = await p.evaluate(() => navigator.clipboard.readText());
    check(`copying the ${kind.toLowerCase()} shows its code, and the button copies it`,
      !!code && clip === (kind === 'FILE' ? code : `reminal paste ${code}`), JSON.stringify(clip));
    console.log(`TRANSFER-${kind} ${code}`);
    await clearNotifications();
  }
}

if (phase === 'paste') {
  // reminal Paste on a folder, with a code made on another machine.
  const code = process.env.PASTE_CODE;
  await p.keyboard.press('Control+Shift+E');
  await p.waitForTimeout(1500);
  const box = p.locator('.quick-input-widget .quick-input-box input').first();
  const openPaste = async () => {
    await explorerRow('inbox').click({ button: 'right' });
    await p.waitForTimeout(900);
    await menuPick('reminal Paste…');
    await box.waitFor({ state: 'visible', timeout: 8000 });
  };
  await openPaste();
  await p.keyboard.type('not a code');
  await p.waitForTimeout(600);
  const warn = await p.locator('.quick-input-widget .quick-input-message').first().innerText().catch(() => '');
  await p.screenshot({ path: `${SHOTS}/42-paste-box-bad.png` });
  check('the paste box rejects something that isn\'t a code', /10 letters and numbers/.test(warn), JSON.stringify(warn));
  await box.fill(`reminal paste ${code}`);
  await p.waitForTimeout(400);
  await p.keyboard.press('Enter');
  const done = await waitToast(/Pasted peer-note\.txt into inbox/, 120000).then((t) => t, () => undefined);
  await p.screenshot({ path: `${SHOTS}/43-pasted.png` });
  check('a whole "reminal paste <code>" line pasted into the folder', !!done, done ? (await done.innerText()).split('\n')[0] : '');
  await clearNotifications();
  // A well-formed code that was never handed out.
  await openPaste();
  await box.fill('ABCDE-FGHJK');
  await p.keyboard.press('Enter');
  const bad = await waitToast(/reminal couldn't paste/, 120000).then((t) => t, () => undefined);
  await p.screenshot({ path: `${SHOTS}/44-paste-unknown-code.png` });
  check('an unknown code says reminal couldn\'t paste', !!bad, bad ? (await bad.innerText()).split('\n')[0] : '');
  await clearNotifications();
  // A paste that fails in a folder named like a link, [docs](command:...)
  // (run.sh made it read-only): the error names the folder, and the
  // notification must show it as text, not as a link.
  await explorerRow('[docs](command:workbench.action.showCommands)').click({ button: 'right' });
  await p.waitForTimeout(900);
  await menuPick('reminal Paste…');
  await box.waitFor({ state: 'visible', timeout: 8000 });
  await box.fill(process.env.PASTE_CODE2 ?? '');
  await p.keyboard.press('Enter');
  const odd = await waitToast(/reminal couldn't paste/, 120000).then((t) => t, () => undefined);
  await p.screenshot({ path: `${SHOTS}/44b-paste-error-link-named-folder.png` });
  const msg = odd ? odd.locator('.notification-list-item-message') : undefined;
  const linkCount = msg ? await msg.locator('a').count() : -1;
  const text = msg ? (await msg.innerText()).replace(/\s+/g, ' ') : '';
  check('a paste error naming a folder like a link shows it as plain text', !!odd && linkCount === 0 && /docs\(command:/.test(text), `links ${linkCount}: ${JSON.stringify(text)}`);
  await clearNotifications();
}

if (phase === 'ports') {
  // Ports reminal shares, in the cloud button's list (against the hosted
  // relay: the local one doesn't carry ports). run.sh serves a marker page
  // on port 3000.
  const marker = process.env.PORT_MARKER;
  // The cloud button is in the terminal panel's title bar.
  await runCommand('Terminal: Create New Terminal');
  await p.waitForTimeout(2500);
  const sharedRows = () => listRows().filter({ hasText: 'localhost:3000' });
  await openList();
  const before = await sharedRows().count();
  const shareRow = listRows().filter({ hasText: 'Share a Port' });
  check('before anything is shared, the list has no port, and a Share a Port row', before === 0 && (await shareRow.count()) === 1);
  await shareRow.first().click();
  const box = p.locator('.quick-input-widget .quick-input-box input').first();
  await box.waitFor({ state: 'visible', timeout: 8000 });
  await p.keyboard.type('70000');
  await p.waitForTimeout(600);
  const warn = await p.locator('.quick-input-widget .quick-input-message').first().innerText().catch(() => '');
  check('Share a Port rejects a port that doesn\'t exist', /1 to 65535/.test(warn), JSON.stringify(warn));
  await box.fill('3000');
  await p.keyboard.press('Enter');
  const shared = await waitToast(/localhost:3000 is shared at https:\/\//, 60000);
  await p.screenshot({ path: `${SHOTS}/45-port-shared.png` });
  await shared.getByRole('button', { name: 'Copy link' }).click();
  await p.waitForTimeout(500);
  const link = await p.evaluate(() => navigator.clipboard.readText());
  const pageSays = async (url) => {
    const v = await ctx.newPage();
    const ok = await v.goto(url, { waitUntil: 'domcontentloaded', timeout: 30000 })
      .then(() => v.waitForFunction((m) => document.body.innerText.includes(m), marker, { timeout: 20000 }))
      .then(() => true, () => false);
    await v.close();
    return ok;
  };
  check('Share a Port shares it: its copied link opens the page', await pageSays(link), link);
  await clearNotifications();
  await p.waitForTimeout(5000);
  await openList();
  const rows = await sharedRows().count();
  await p.screenshot({ path: `${SHOTS}/46-list-with-port.png` });
  const section = await p.locator('.quick-input-widget .quick-input-list').innerText();
  check('the cloud list shows the shared port under Shared ports', rows === 1 && /Shared ports/i.test(section), JSON.stringify(section.replace(/\s+/g, ' ').slice(0, 200)));
  await sharedRows().first().click();
  const actions = await waitToast(/localhost:3000 is shared at/, 8000);
  await actions.getByRole('button', { name: 'Stop sharing' }).click();
  const stopped = await waitToast(/Stopped sharing localhost:3000/, 30000).then(() => true, () => false);
  await p.screenshot({ path: `${SHOTS}/47-port-stopped.png` });
  check('Stop sharing stops it: the link no longer opens the page', stopped && !(await pageSays(link)));
  await clearNotifications();
  await p.waitForTimeout(5000);
  await openList();
  check('a stopped port leaves the list', (await sharedRows().count()) === 0);
  await p.keyboard.press('Escape');
}

} catch (e) {
  await p.screenshot({ path: `${SHOTS}/failed-${phase}.png` }).catch(() => {});
  check(`${phase}: ${e.message.split('\n')[0]}`, false);
}
}
console.log(out.join('\n'));
console.log('DONE');
await b.close();
process.exit(fail ? 1 : 0);
