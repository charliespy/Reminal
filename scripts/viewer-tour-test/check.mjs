// run.sh: the first-visit tour and the terminal's keyboard.
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync } from 'node:fs';
const html = readFileSync('/t/index.html');
http.createServer((q, r) => { r.writeHead(200, { 'content-type': 'text/html' }); r.end(html); }).listen(8099);
const b = await chromium.launch();
let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };

async function open(seen) {
  const ctx = await b.newContext({ viewport: { width: 1100, height: 700 } });
  if (seen) await ctx.addInitScript(() => { try { localStorage.setItem('reminal-tour-v1', 'done'); } catch (_) {} });
  const page = await ctx.newPage();
  await page.goto('http://127.0.0.1:8099/');
  await page.waitForFunction(() => typeof term === 'object' && term && document.querySelector('.xterm-helper-textarea'));
  // What is sent to the session (the socket stood in for, the session live).
  await page.evaluate(() => { window.__got = ''; inputLive = () => true; deliverInput = (d) => { window.__got += d; }; });
  return { ctx, page };
}
const tourOpen = (page) => page.evaluate(() => !!document.querySelector('.tour-card'));
// Which step the card shows: its counter ("02 / 09"), or its heading on a
// step without one (the welcome).
const step = (page) => page.evaluate(() => {
  const t = document.querySelector('.tour-card')?.innerText || '';
  return (t.match(/(\d\d)\s*\/\s*\d\d/) || [])[1] || t.split('\n')[0];
});

// ---- Opened while the terminal has the keyboard ----
{
  const { ctx, page } = await open(true);
  await page.locator('.xterm-helper-textarea').focus();
  await page.evaluate(() => window.reminalTour.start());
  await page.waitForFunction(() => !!document.querySelector('.tour-card'));
  await page.waitForTimeout(400);
  const s0 = await step(page);
  const focus = await page.evaluate(() => document.activeElement && document.activeElement.className);
  /xterm-helper-textarea/.test(focus || '') ? ok('the terminal keeps the keyboard when the tour opens') : fail('the tour took the keyboard: focus is on ' + focus);
  await page.keyboard.type('echo hi', { delay: 30 });
  await page.keyboard.press('Enter');
  await page.keyboard.press('ArrowUp');
  await page.waitForTimeout(200);
  const got = await page.evaluate(() => window.__got);
  got === 'echo hi\r\x1b[A' ? ok('typed, Enter and an arrow all reach the session while the tour is open') : fail('what reached the session while the tour was open: ' + JSON.stringify(got));
  const s1 = await step(page);
  s1 === s0 && await tourOpen(page) ? ok(`the tour stays on its step (${s0}), unmoved by keys meant for the terminal`) : fail(`the tour moved (${s0} → ${s1}, open ${await tourOpen(page)}) on the terminal's keys`);
  // The card answers once clicked into: Next, then its own arrow key.
  await page.locator('.tour-card .tour-btn1').click();
  await page.waitForTimeout(400);
  const s2 = await step(page);
  await page.keyboard.press('ArrowRight');
  await page.waitForTimeout(400);
  const s3 = await step(page);
  s2 !== s1 && s3 !== s2 ? ok(`clicked into, the card goes on with its buttons and keys (${s1} → ${s2} → ${s3})`) : fail(`the card after a click: ${s2}, after ArrowRight: ${s3}`);
  await ctx.close();
}

// ---- A first visit: not while typing, in a pause ----
{
  const { ctx, page } = await open(false);
  await page.locator('.xterm-helper-textarea').focus();
  let openedWhileTyping = false;
  for (let i = 0; i < 12; i++) {           // typing for ~3.6 s
    await page.keyboard.press('KeyA');
    await page.waitForTimeout(300);
    if (await tourOpen(page)) openedWhileTyping = true;
  }
  !openedWhileTyping ? ok('the first-visit tour does not open while the terminal is being typed into') : fail('the first-visit tour opened over the typing');
  await page.waitForTimeout(3000);           // a pause
  await tourOpen(page) ? ok('it opens once the typing pauses') : fail('it never opened after the typing paused');
  await ctx.close();
}
await b.close();
console.log(fails ? `FAIL (${fails})` : 'PASS');
process.exit(fails);
