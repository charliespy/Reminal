// e2e.sh: sleep and wake from the viewer, as an owner device of the machine.
import { chromium } from 'playwright';
import { execSync, spawn } from 'node:child_process';
import { existsSync, readFileSync, readdirSync } from 'node:fs';

let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const check = (c, good, why) => (c ? ok(good) : fail(good + ' — ' + why));
const sh = (c) => execSync(c, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
async function until(fn, secs = 30) {
  for (const end = Date.now() + secs * 1000; Date.now() < end; await sleep(300)) { try { if (await fn()) return true; } catch (_) {} }
  return false;
}
const restore = (id, suf) => '/root/.reminal/restore/' + id + suf;
const convs = () => readdirSync('/root/.fake-claude').filter(f => f.endsWith('.conv')).map(f => readFileSync('/root/.fake-claude/' + f, 'utf8')).join('\n');

spawn('reminal', ['relay', '8080'], { detached: true, stdio: 'ignore' }).unref();
if (!(await until(async () => (await fetch('http://127.0.0.1:8080/')).ok, 20))) { fail('the relay did not start'); process.exit(1); }
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 } });
await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
const p = await ctx.newPage();
const errs = [];
p.on('pageerror', e => errs.push(e.message));
let dialogs = [];
p.on('dialog', d => { dialogs.push(d.message()); d.accept(); });
await p.goto('http://127.0.0.1:8080/');
await p.waitForFunction(() => typeof ownDeviceId === 'function' && typeof queryDirectory === 'function');
sh(`reminal add owner ${await p.evaluate(() => ownDeviceId())} -y`);
spawn('reminal', ['daemon'], { detached: true, stdio: 'ignore' }).unref();
await sleep(1500);
sh('mkdir -p /root/work');
const made = sh('cd /root/work && reminal new work');
const sid = made.match(/Session:\s+([A-Z0-9]+)/)[1], pin = made.match(/PIN:\s*([0-9]+)/)[1];
await p.evaluate(s => connectOwned(s), sid);
check(await until(() => p.evaluate(async () => Object.keys(await loadOwnedMachines()).length === 1), 40), 'the browser owns the machine', 'no machine');
await until(() => p.evaluate(() => document.body.classList.contains('connected')), 20);
await sleep(800);
// A stand-in claude with a conversation in it, typed as a person would.
await p.evaluate(() => term.focus());
await p.keyboard.type('echo the-marker-before-sleep\n', { delay: 10 });
await p.keyboard.type('claude\n', { delay: 10 });
await sleep(2000);
await p.keyboard.type('remember 4242\n', { delay: 10 });
await sleep(2000);
check(convs().includes('you said: remember 4242'), 'claude is running with a conversation', convs());

// A guest, by link and PIN, watching it: through the sleep and the wake.
const gctx = await browser.newContext();
await gctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
const g = await gctx.newPage();
g.on('pageerror', e => errs.push('guest: ' + e.message));
await g.goto(`http://127.0.0.1:8080/?s=${sid}#p=${pin}`);
check(await until(() => g.evaluate(() => document.body.classList.contains('connected')), 30), 'a guest viewer is watching it by link and PIN', 'guest not connected');
const screenOf = (pg) => pg.evaluate(() => { const b = term.buffer.active; let t = ''; for (let y = 0; y < b.length; y++) t += b.getLine(y).translateToString(true) + '\n'; return t; });
const key = await p.evaluate(async () => Object.keys(await loadOwnedMachines())[0]);
const listing = () => p.evaluate(async (k) => (await queryDirectory(base64ToU8(b64URLToStd(k)), {})).sessions, key);
const row = async (id) => (await listing()).find(s => s.id === id);

// Sleep from the viewer.
dialogs = []; // the first connect's "remember this machine?" is not the sleep's
await p.evaluate(async ([k, s]) => sleepOnMachine(k, s), [key, await row(sid)]);
check(await until(async () => (await row(sid))?.asleep, 20), 'Sleep from the viewer: the machine lists it asleep', JSON.stringify(await row(sid)));
check(existsSync(restore(sid, '.asleep')) && existsSync(restore(sid, '.sealed')), 'its record is kept, marked asleep', 'no record or mark');
check(!dialogs.length, 'nothing to let go of, so nothing was asked', dialogs.join(' | '));
// Its row: asleep, opened by waking, no rename.
const rowShape = await p.evaluate(([k, s]) => {
  const box = document.createElement('div');
  box.innerHTML = '<span class="machine-count"></span><div class="machine-sessions"></div>';
  renderMachineSessions(box, [s], k);
  const r = box.querySelector('.machine-session');
  return { asleep: r.classList.contains('is-asleep'), chip: r.querySelector('.ms-state')?.textContent, meta: r.querySelector('.ms-meta')?.textContent, buttons: [...r.querySelectorAll('.ms-btn')].map(b => b.title.split(' ')[0]) };
}, [key, await row(sid)]);
check(rowShape.asleep && rowShape.chip === 'asleep' && /open it to wake it/.test(rowShape.meta), 'its row says asleep, open it to wake it', JSON.stringify(rowShape));
check(!rowShape.buttons.some(b => /^Rename/.test(b)) && !rowShape.buttons.some(b => /^Sleep/.test(b)), 'no rename or sleep on a sleeping row', JSON.stringify(rowShape.buttons));

// Wake by opening it: its saved screen shows, and keys typed meanwhile wait
// for claude, then go to it, not to the shell it starts in.
const s0 = await row(sid);
const woke = p.evaluate(async ([k, s]) => wakeOwned(k, s), [key, s0]);
check(await until(() => p.evaluate(() => !document.getElementById('wake-screen').hidden), 10), 'opening it shows "waking"', 'no wake screen');
await woke;
const shown = await p.evaluate(() => document.querySelector('#wake-screen .wake-rows').textContent);
check(/the-marker-before-sleep|remember 4242/.test(shown), 'its last saved screen was shown while it woke', shown.slice(-200));
await p.evaluate(() => term.focus());
await p.keyboard.type('typed-while-waking\n', { delay: 5 });
check(await until(() => convs().includes('you said: typed-while-waking'), 40), 'keys typed while it woke went to claude once it was back', convs().split('\n').slice(-4).join(' | '));
check(await until(async () => !(await row(sid))?.asleep, 10), 'awake again, same id', JSON.stringify(await row(sid)));
check(!existsSync(restore(sid, '.asleep')), 'no longer marked asleep', 'still marked');
check(await p.evaluate(() => document.getElementById('wake-screen').hidden), 'the live terminal replaced the saved screen', 'wake screen still up');
check(await until(async () => (await screenOf(g)).includes('typed-while-waking'), 30), 'the guest viewer, open through it all, reconnected and sees it live', (await screenOf(g)).slice(-300));

// A job in it: asked, and slept anyway only when said yes; the row says what went.
await p.keyboard.type('\x04', { delay: 5 }); await sleep(800);       // leave claude
await p.keyboard.type('sleep 600 &\n', { delay: 5 }); await sleep(800);
dialogs = [];
await p.evaluate(async ([k, s]) => sleepOnMachine(k, s), [key, await row(sid)]);
check(dialogs.some(d => /sleep, running in it/.test(d) && /Sleep anyway/.test(d)), 'a job in it: the viewer asks, naming it', dialogs.join(' | '));
check(await until(async () => (await row(sid))?.asleep, 20), 'said yes: it sleeps', JSON.stringify(await row(sid)));
check(((await row(sid))?.dropped || []).some(d => /sleep, running in it/.test(d)), 'the listing says what it let go of', JSON.stringify(await row(sid)));

// Kill on a sleeping session forgets it.
await p.evaluate(async ([k, id]) => queryDirectory(base64ToU8(b64URLToStd(k)), { kill: id }), [key, sid]);
check(await until(async () => !(await row(sid)), 10), 'kill on a sleeping session forgets it', JSON.stringify(await row(sid)));
check(!existsSync(restore(sid, '.sealed')) && !existsSync(restore(sid, '.asleep')), 'its record and mark are gone', 'left behind');

check(!errs.length, 'no page errors', errs.join(' | '));
await browser.close();
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
