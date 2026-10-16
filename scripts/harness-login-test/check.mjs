// run.sh: each agent CLI, with no login, listed "logged out" by its session.
import { execSync, spawn } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';

let fails = 0;
const ok = (m) => console.log('ok   ' + m);
const fail = (m) => { console.log('FAIL ' + m); fails++; };
const sh = (c, env) => execSync(c, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, ...env } });
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
// Typed into a session as reminal's MCP send_keys does.
function mcp(name, args) {
  return new Promise((resolve) => {
    const p = spawn('reminal', ['mcp']);
    let out = '';
    p.stdout.on('data', d => { out += d; if (out.includes('"id":2')) { p.kill(); resolve(out); } });
    const send = (o) => p.stdin.write(JSON.stringify(o) + '\n');
    send({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'rig', version: '1' } } });
    send({ jsonrpc: '2.0', method: 'notifications/initialized' });
    send({ jsonrpc: '2.0', id: 9, method: 'tools/list' });
    send({ jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name, arguments: args } });
    setTimeout(() => { p.kill(); resolve(out); }, 10000);
  });
}
async function screen(session) {
  const out = await mcp('read_transcript', { session, lines: 60 });
  const line = out.split('\n').find(l => l.includes('"id":2')) || '';
  try { return JSON.parse(line).result.content.map(c => c.text).join(''); } catch (_) { return line.slice(0, 2000); }
}
function keys(session, text, enter = true) {
  return new Promise((resolve) => {
    const p = spawn('reminal', ['mcp']);
    let out = '';
    p.stdout.on('data', d => { out += d; if (out.includes('"id":2')) { p.kill(); resolve(out); } });
    const send = (o) => p.stdin.write(JSON.stringify(o) + '\n');
    send({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2024-11-05', capabilities: {}, clientInfo: { name: 'rig', version: '1' } } });
    send({ jsonrpc: '2.0', method: 'notifications/initialized' });
    send({ jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'send_keys', arguments: { session, keys: text, enter } } });
    setTimeout(() => { p.kill(); resolve(out); }, 10000);
  });
}
const stateOf = (sid) => { const row = sh('reminal list 2>/dev/null').split('\n').find(l => l.includes(sid)) || ''; return /logged out/.test(row) ? 'logged out' : row.trim().replace(/\s+/g, ' ').slice(0, 90); };

// Each case: a home of its own (no login there), what is set up in it, the
// command, and whether it is then typed a line (a login it cannot use shows
// only on a turn) — and whether it must read logged out.
const W = (o) => JSON.stringify(o);
const CASES = [
  { name: 'cursor', cmd: 'cursor-agent', out: true },
  { name: 'codex', cmd: 'codex', out: true },
  { name: 'codex-key', files: { '.codex/auth.json': W({ OPENAI_API_KEY: 'sk-proj-bad000' }), '.codex/config.toml': 'preferred_auth_method = "apikey"\n' }, cmd: 'codex', turn: true, out: true },
  { name: 'gemini', files: { '.gemini/settings.json': W({ security: { folderTrust: { enabled: false } }, general: { disableAutoUpdate: true } }) }, cmd: 'gemini', out: true },
  { name: 'gemini-key', files: { '.gemini/settings.json': W({ security: { folderTrust: { enabled: false }, auth: { selectedType: 'gemini-api-key' } }, general: { disableAutoUpdate: true } }) }, env: 'GEMINI_API_KEY=AIzaSyBADKEY00000000000000000000000000', cmd: 'gemini', turn: true, out: true },
  { name: 'qwen', cmd: 'qwen', out: true },
  { name: 'opencode', files: { '.config/opencode/opencode.json': W({ autoupdate: false }) }, cmd: 'opencode', out: false },
];
const table = [];
for (const c of CASES.filter(c => !process.env.ONLY || c.name === process.env.ONLY)) {
  const home = '/root/h-' + c.name;
  mkdirSync(home + '/work', { recursive: true });
  for (const [rel, body] of Object.entries(c.files || {})) { mkdirSync(home + '/' + rel.replace(/\/[^/]*$/, ''), { recursive: true }); writeFileSync(home + '/' + rel, body); }
  const sid = sh(`cd ${home}/work && reminal new ${c.name}`, { HOME: '/root' }).match(/Session:\s+([A-Z0-9]+)/)[1];
  await keys(sid, `export HOME=${home}; cd ${home}/work; ${c.env ? 'export ' + c.env + '; ' : ''}${c.cmd}`);
  await sleep(12000);
  if (c.turn) { await keys(sid, 'hello'); await sleep(15000); }
  let state = '';
  for (let i = 0; i < 20 && (state = stateOf(sid)) !== 'logged out' && c.out; i++) await sleep(1000);
  if (!c.out) state = stateOf(sid);
  const got = state === 'logged out';
  if (got !== c.out) console.log('--- ' + c.name + ' screen:\n' + (await screen(sid)).split('\n').slice(-45).join('\n'));
  (got === c.out ? ok : fail)(`${c.name}: ${got ? 'listed logged out' : 'not logged out (' + state + ')'}${c.out ? '' : ' — it needs no login'}`);
  table.push([c.name, got]);
}
// Logged in again (as after signing in), then left alone at its prompt: no
// longer listed logged out. Codex pointed at a local stand-in API.
if (!process.env.ONLY || process.env.ONLY === 'codex-back') {
  const { createServer } = await import('node:http');
  createServer((q, r) => { q.resume(); q.on('end', () => { r.writeHead(500); r.end('{}'); }); }).listen(9100, '127.0.0.1');
  const home = '/root/h-codex-back';
  mkdirSync(home + '/work', { recursive: true });
  const sid = sh(`cd ${home}/work && reminal new codex-back`, { HOME: '/root' }).match(/Session:\s+([A-Z0-9]+)/)[1];
  await keys(sid, `export HOME=${home}; cd ${home}/work; codex`);
  let down = false;
  for (let i = 0; i < 30 && !(down = stateOf(sid) === 'logged out'); i++) await sleep(1000);
  mkdirSync(home + '/.codex', { recursive: true });
  writeFileSync(home + '/.codex/auth.json', JSON.stringify({ OPENAI_API_KEY: 'sk-stand-in' }));
  writeFileSync(home + '/.codex/config.toml', 'check_for_update_on_startup = false\nmodel_provider = "stub"\nmodel = "gpt-5"\n[model_providers.stub]\nname = "stub"\nbase_url = "http://127.0.0.1:9100/v1"\nenv_key = "OPENAI_API_KEY"\nwire_api = "responses"\n');
  try { sh('pkill -f "codex"'); } catch (_) {}
  await sleep(1500);
  await keys(sid, 'OPENAI_API_KEY=sk-stand-in codex');
  let up = false;
  for (let i = 0; i < 20 && !(up = stateOf(sid) !== 'logged out'); i++) await sleep(1000);
  (down && up ? ok : fail)(`codex-back: logged out ${down}, then logged in and idle: ${up ? 'no longer logged out (' + stateOf(sid) + ')' : 'still logged out'}`);
}
console.log(fails ? fails + ' failed' : 'all passed');
process.exit(fails ? 1 : 0);
