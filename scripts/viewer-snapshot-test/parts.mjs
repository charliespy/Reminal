// The viewer's side of a snapshot in parts, driven in the page as served with
// no host behind it (run-parts.sh): parts are gathered and painted once;
// output that comes meanwhile is played after the paint, what the snapshot
// already shows dropped; a part that never comes is never a half paint — the
// plain snapshot is asked for instead; parts for another viewer are ignored.
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync } from 'node:fs';
const html = readFileSync('/t/index.html');
const srv = http.createServer((q, r) => { r.writeHead(200, { 'content-type': 'text/html' }); r.end(html); }).listen(8099);
const b = await chromium.launch();
const page = await b.newPage();
await page.goto('http://127.0.0.1:8099/');
const out = await page.evaluate(async () => {
  // No host: payloads arrive as a sealed frame's do once opened ({__plain}),
  // and what would be written to the terminal or sent to the host is
  // recorded.
  cryptoKey = {};
  const painted = [], resumes = [];
  enqueueData = (bytes) => painted.push(new TextDecoder().decode(bytes));
  ws = { readyState: WebSocket.OPEN, send: (t) => resumes.push(JSON.parse(t)) };
  const enc = (s) => new TextEncoder().encode(s);
  const gzip = async (s) => new Uint8Array(await new Response(new Blob([enc(s)]).stream().pipeThrough(new CompressionStream('gzip'))).arrayBuffer());
  const part = (id, i, n, chunk, to = viewerId) => {
    const h = enc(JSON.stringify({ id, i, n, to }));
    const b = new Uint8Array(4 + h.length + chunk.length);
    new DataView(b.buffer).setUint32(0, h.length);
    b.set(h, 4); b.set(chunk, 4 + h.length);
    return { __plain: b };
  };
  const data = (seq, s) => handleMessage({ type: 'data', seq, data: { __plain: enc(s) } });
  const reset = () => { painted.length = 0; resumes.length = 0; lastSeq = 0; awaitingSnapshotUntil = Date.now() + 15000; snapAsm = null; snapPartsOff = false; };
  const res = {};

  // In two parts, with output arriving between them.
  reset();
  const snap = '\x1b[2J\x1b[3J' + Array.from({ length: 50 }, (_, i) => 'line ' + (i + 1)).join('\r\n');
  const gz = await gzip(snap), cut = Math.floor(gz.length / 2);
  await handleMessage({ type: 'snapshot_part', seq: 100, data: part('A', 0, 2, gz.slice(0, cut)) });
  await data(99, 'OLD');   // already in the snapshot: dropped
  await data(101, 'NEW1'); // after it: played after the paint
  const before = painted.length;
  await handleMessage({ type: 'snapshot_part', seq: 100, data: part('A', 1, 2, gz.slice(cut)) });
  await data(102, 'NEW2');
  res.twoParts = { paintedBeforeLastPart: before, order: painted.map((p) => p.startsWith('\x1b[2J') ? 'SNAPSHOT' : p), lastSeq };

  // Parts for another viewer.
  reset();
  await handleMessage({ type: 'snapshot_part', seq: 100, data: part('B', 0, 1, gz, 'v-someone-else') });
  res.otherViewer = { painted: painted.length, assembling: !!snapAsm };

  // A part that never comes.
  reset();
  await handleMessage({ type: 'snapshot_part', seq: 100, data: part('C', 0, 2, gz.slice(0, cut)) });
  await data(101, 'MEANWHILE');
  snapPartsGiveUp(); // what the wait's timer calls
  res.missingPart = { painted: painted.length, resume: resumes[resumes.length - 1] || null, lastSeq, assembling: !!snapAsm, partsOff: snapPartsOff };
  // The resume a viewer sends now asks for the plain snapshot.
  return res;
});
console.log(JSON.stringify(out));
const t = out.twoParts, o = out.otherViewer, m = out.missingPart;
const checks = [
  ['nothing painted before the last part', t.paintedBeforeLastPart === 0],
  ['the snapshot painted once, then only what came after it', JSON.stringify(t.order) === JSON.stringify(['SNAPSHOT', 'NEW1', 'NEW2'])],
  ['parts for another viewer are not painted or gathered', o.painted === 0 && !o.assembling],
  ['a missing part: nothing painted', m.painted === 0 && !m.assembling],
  ['a missing part: the plain snapshot asked for (resume from 0, no parts)', m.resume && m.resume.type === 'resume' && m.resume.from_seq === 0 && !m.resume.snap && m.partsOff],
];
for (const [n, ok] of checks) console.log((ok ? 'PASS  ' : 'FAIL  ') + n);
await b.close(); srv.close();
process.exit(checks.every(([, ok]) => ok) ? 0 : 1);
