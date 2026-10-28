// run.sh: a stand-in Claude Code at its usage limit is listed "limited"; one
// that finished its turn stays "done".
import { execSync, spawn } from 'node:child_process';

let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const sh = (c) => execSync(c, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] });
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
// Typed into a session as reminal's MCP send_keys does.
function keys(session, text) {
  return new Promise((resolve) => {
    const p = spawn('reminal', ['mcp']);
    let out = '';
    p.stdout.on('data', d => { out += d; if (out.includes('"id":2')) { p.kill(); resolve(out); } });
    const send = (o) => p.stdin.write(JSON.stringify(o) + '\n');
    send({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'rig', version: '1' } } });
    send({ jsonrpc: '2.0', method: 'notifications/initialized' });
    send({ jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'send_keys', arguments: { session, keys: text, enter: true } } });
    setTimeout(() => { p.kill(); resolve(out); }, 10000);
  });
}
const attnOf = (sid) => (JSON.parse(sh('reminal info --all --json')).find(s => s.id === sid) || {}).attn || '';

const CASES = [
  { name: 'spend', draft: '', want: 'limited' },
  { name: 'spend', draft: 'c', want: 'limited' },
  { name: 'weekly', draft: '', want: 'limited' },
  { name: 'finished', draft: '', want: 'done' },
];
for (const c of CASES) {
  const sid = sh(`cd /root && reminal new ${c.name}`).match(/Session:\s+([A-Z0-9]+)/)[1];
  await keys(sid, `claude ${c.name} '${c.draft}'`);
  // Settled: the state read is the same for 6s running.
  let state = '', same = 0;
  for (let i = 0; i < 40 && same < 6; i++) {
    await sleep(1000);
    const s = attnOf(sid);
    same = s === state ? same + 1 : 0;
    state = s;
  }
  (state === c.want ? ok : fail)(`${c.name}${c.draft ? ' (draft "' + c.draft + '")' : ''}: ${state || '(none)'}, want ${c.want}`);
}
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
