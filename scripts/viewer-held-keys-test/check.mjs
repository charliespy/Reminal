// Keys typed while a switch to another session is under way are held for
// that session. If the person goes back to the session still on the page
// before the switch has dialled, they were not typed for it: they are
// dropped, never sent down its socket, and the terminal says so. Keys typed for the session on the page
// itself are kept (run.sh).
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync } from 'node:fs';
const html = readFileSync('/t/index.html');
const srv = http.createServer((q, r) => { r.writeHead(200, { 'content-type': 'text/html' }); r.end(html); }).listen(8099);
const b = await chromium.launch();
const ctx = await b.newContext();
await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'skipped'); } catch (_) {} });
const page = await ctx.newPage();
await page.goto('http://127.0.0.1:8099/');
const out = await page.evaluate(async () => {
  openSocket = () => {}; // no relay here: nothing dials
  const held = () => heldInput.map((x) => x.data).join('');
  const back = async (heldFor) => {
    dropHeldInput();
    sessionId = 'XXXX1111'; // the session on the page
    switchingTo = heldFor; // '' : nothing under way; else a switch to it
    holdInput('rm -rf build', ++inputSerial);
    sessionInput.value = 'XXXX1111';
    pinInput.value = '';
    startConnect(); // back to the session on the page; its drop runs before any await
    const r = { held: held(), switchingTo };
    abandonSwitch();
    await new Promise((res) => setTimeout(res, 300)); // the terminal draws what it is given a frame later
    r.said = /What you typed for YYYY2222 was not sent/.test(document.querySelector('.xterm-rows').innerText);
    return r;
  };
  const forOther = await back('YYYY2222');
  term.reset();
  return { forOther, forThis: await back('') };
});
console.log(JSON.stringify(out));
const ok1 = out.forOther.held === '' && out.forOther.switchingTo === '' && out.forOther.said;
const ok2 = out.forThis.held === 'rm -rf build' && !out.forThis.said;
console.log((ok1 ? 'PASS  ' : 'FAIL  ') + 'keys held for a session being switched to are dropped, and said so, when the person goes back to the one on the page');
console.log((ok2 ? 'PASS  ' : 'FAIL  ') + 'keys typed for the session on the page are kept when it connects again');
await b.close(); srv.close();
process.exit(ok1 && ok2 ? 0 : 1);
