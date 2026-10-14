// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// The relay's own count of who is connected, as the agent hears it
// (`reminal connections`): one while a page is open, still one after that
// page's socket closed and it dialled again (the closed one gone from it),
// none once the page is gone.
import { chromium, devices } from 'playwright';
import { execSync } from 'node:child_process';
const BASE = 'http://localhost:8787';
const [S, PIN] = process.argv.slice(2);
const count = () => {
  const out = execSync(process.env.CONNECTIONS, { encoding: 'utf8' });
  const m = /(\d+) viewers? attached/.exec(out);
  return m ? Number(m[1]) : /no viewers attached/.test(out) ? 0 : NaN;
};
const settle = async (want) => { for (let i = 0; i < 40; i++) { if (count() === want) return want; await new Promise(r => setTimeout(r, 250)); } return count(); };
const b = await chromium.launch();
const ctx = await b.newContext({ ...devices['Pixel 7'] });
const p = await ctx.newPage();
await p.goto(`${BASE}/?s=${S}#p=${PIN}`, { waitUntil: 'domcontentloaded' });
await p.waitForFunction(() => inputLive(), null, { timeout: 60000 });
const open = await settle(1);
await p.evaluate(() => ws.close());
await p.waitForFunction(() => !inputLive(), null, { timeout: 15000 });
await p.waitForFunction(() => inputLive(), null, { timeout: 60000 });
const again = await settle(1);
await ctx.close();
const gone = await settle(0);
await b.close();
const ok = open === 1 && again === 1 && gone === 0;
console.log(`5  viewers the agent hears of: ${open} with the page open, ${again} after its socket closed and it dialled again, ${gone} once it is gone   ${ok ? 'PASS' : 'FAIL'}`);
process.exit(ok ? 0 : 1);
