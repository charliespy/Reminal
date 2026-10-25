// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar
//
// The upload toast (run.sh), driven on the page as served: a socket that
// drains slowly stands in for the network, and the machine's acks come in
// through handleMessage as upload_ack, sealed with the session key.
import { chromium } from 'playwright';
import http from 'node:http';
import { readFileSync } from 'node:fs';
const html = readFileSync('/t/index.html');
const srv = http.createServer((q, r) => { const a = /^\/assets\/([\w.-]+\.js)$/.exec(q.url.split('?')[0]); if (a) { let b; try { b = readFileSync('/t/assets/' + a[1]); } catch (_) { r.writeHead(404); return r.end(); } r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(b); } if (q.url.split('?')[0] === '/reload.js') { r.writeHead(200, { 'content-type': 'text/javascript' }); return r.end(readFileSync('/t/reload.js')); } r.writeHead(200, { 'content-type': 'text/html' }); r.end(html); }).listen(8099);
const b = await chromium.launch();
const page = await b.newPage();
await page.goto('http://127.0.0.1:8099/');
const out = await page.evaluate(async () => {
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
  const box = () => document.getElementById('upload-toasts');
  const toast = () => { const t = box() && box().querySelector('.up-toast'); return t ? {
    cls: t.className, title: t.querySelector('.up-title').textContent, meta: t.querySelector('.up-meta').textContent,
    width: parseFloat(t.querySelector('.up-fill').style.width) || 0, wait: t.querySelector('.up-fill').classList.contains('wait'),
    skip: (t.querySelector('.up-skip') || {}).textContent || '', dismiss: !!t.querySelector('.up-dismiss'),
    gone: t.classList.contains('gone') } : null; };
  // A socket whose sends sit in bufferedAmount and drain at `rate` bytes
  // every 50 ms, as on a slow link.
  const slowSock = (rate) => {
    const s = { readyState: WebSocket.OPEN, sent: [], bufferedAmount: 0, send(f) { this.sent.push(f); this.bufferedAmount += f.length; } };
    s.timer = setInterval(() => { s.bufferedAmount = Math.max(0, s.bufferedAmount - rate); }, 50);
    return s;
  };
  // The upload ids of the files sent on sock, in order.
  const idsOn = async (sock) => {
    const ids = [];
    for (const f of sock.sent) {
      const m = JSON.parse(f);
      if (m.type !== 'upload') continue;
      const p = JSON.parse(new TextDecoder().decode(await decrypt(key, m.data)));
      if (!ids.includes(p.upload_id)) ids.push(p.upload_id);
    }
    return ids;
  };
  const ack = async (id, path) => handleMessage({ type: 'upload_ack',
    data: await encrypt(key, new TextEncoder().encode(JSON.stringify({ upload_id: id, path }))) });
  const file = (n, size) => new File([new Uint8Array(size)], n);
  window.prompt = () => '1h';
  window.alert = () => {};
  const res = {};

  // A: one file of three chunks on a slow link.
  authed = true; cryptoKey = key; ws = slowSock(48 * 1024);
  const sock = ws;
  const p = uploadFile(file('big.bin', 3 * UPLOAD_CHUNK_BYTES - 7), 0);
  const seen = [], widths = [];
  let handedAll = null;
  for (let i = 0; i < 200; i++) {
    await sleep(40);
    const t = toast();
    if (!t) continue;
    seen.push(t.title);
    if (/^Uploading/.test(t.title)) widths.push(t.width);
    if (sock.sent.length === 3 && sock.bufferedAmount > 0 && /^Uploading/.test(t.title) && handedAll === null) handedAll = t.width;
    if (/^Saving/.test(t.title)) break;
  }
  await p;
  res.a = { titles: [...new Set(seen)], handedAllWidth: handedAll, widths, saving: toast() };
  const [idA] = await idsOn(sock);
  await ack(idA, '/home/u/Downloads/reminal/big.bin');
  await sleep(100);
  res.a.done = toast();
  await sleep(4600);
  res.a.after = toast();
  clearInterval(sock.timer);

  // B: a batch of three, the middle one too large; two acked.
  ws = slowSock(1 << 22);
  const sockB = ws;
  const mb = new Uint8Array(1 << 20);
  const huge = new File(Array.from({ length: (UPLOAD_MAX_BYTES >> 20) + 1 }, () => mb), 'huge.bin');
  await uploadBatch([file('one.txt', 10), huge, file('three.txt', 10)]);
  res.b = { during: toast() };
  for (const [i, id] of (await idsOn(sockB)).entries()) await ack(id, '/home/u/Downloads/reminal/' + ['one.txt', 'three.txt'][i]);
  await sleep(100);
  res.b.done = toast();
  await sleep(7000);
  res.b.after = toast();
  clearInterval(sockB.timer);

  // C: a batch of two, both acked.
  ws = slowSock(1 << 22);
  const sockC = ws;
  await uploadBatch([file('a.png', 10), file('b.png', 10)]);
  for (const [i, id] of (await idsOn(sockC)).entries()) await ack(id, '/home/u/Downloads/reminal/' + ['a.png', 'b.png'][i]);
  await sleep(100);
  res.c = { done: toast() };
  clearInterval(sockC.timer);

  // D: the session left mid-file (the viewer on another socket).
  const sockD = slowSock(1 << 22);
  const next = slowSock(1 << 22);
  sockD.send = function (f) { this.sent.push(f); if (this.sent.length === 1) ws = next; };
  ws = sockD;
  await uploadFile(file('backup.zip', 3 * UPLOAD_CHUNK_BYTES), 0);
  res.d = { now: toast() };
  await sleep(6600);
  res.d.after = toast();
  clearInterval(sockD.timer); clearInterval(next.timer);
  ws = null; cryptoKey = null; authed = false;
  return res;
});
console.log(JSON.stringify(out, null, 1));
const fails = [];
const a = out.a;
if (!a.titles.some(t => t === 'Uploading big.bin')) fails.push('A: never said "Uploading big.bin" (' + a.titles.join(' | ') + ')');
if (a.handedAllWidth === null) fails.push('A: no reading while every chunk was handed and bytes were still queued');
else if (a.handedAllWidth >= 95) fails.push('A: the bar read ' + a.handedAllWidth + '% with bytes still queued in the socket (counts queued, not sent)');
if (!a.widths.some(w => w > 5 && w < 95)) fails.push('A: the bar never read between 5% and 95% while sending (' + a.widths.join(',') + ')');
if (!a.saving || !/^Saving big\.bin on the machine/.test(a.saving.title) || !a.saving.wait) fails.push('A: after the last byte, not "Saving big.bin on the machine" (' + JSON.stringify(a.saving) + ')');
if (!a.done || !/\bok\b/.test(a.done.cls) || a.done.title !== 'Uploaded big.bin' || !a.done.meta.includes('Saved to /home/u/Downloads/reminal/big.bin')) fails.push('A: on the ack, not done with where it landed (' + JSON.stringify(a.done) + ')');
if (a.after && !a.after.gone) fails.push('A: done did not fade');
const bb = out.b;
if (!bb.during || !/huge\.bin is too large/.test(bb.during.skip + bb.during.meta)) fails.push('B: the file too large was not said while the batch went on (' + JSON.stringify(bb.during) + ')');
if (!bb.done || !/\berr\b/.test(bb.done.cls) || bb.done.title !== '2 of 3 files uploaded' || !/huge\.bin is too large/.test(bb.done.meta) || !bb.done.dismiss) fails.push('B: the batch end did not say 2 of 3 and the file too large, with Dismiss (' + JSON.stringify(bb.done) + ')');
if (!bb.after || bb.after.gone) fails.push('B: a batch with a file too large did not stay until dismissed');
const c = out.c;
if (!c.done || c.done.title !== '2 files uploaded' || !c.done.meta.includes('Saved to /home/u/Downloads/reminal/ · paths typed at the cursor')) fails.push('C: two files acked, not "2 files uploaded" with their folder (' + JSON.stringify(c.done) + ')');
const d = out.d;
if (!d.now || d.now.title !== 'Upload stopped: you left the session' || d.now.dismiss) fails.push('D: a session left mid-upload not said, or asks to be dismissed (' + JSON.stringify(d.now) + ')');
if (d.after && !d.after.gone) fails.push('D: a session left mid-upload did not fade');
console.log(fails.length ? 'FAIL  ' + fails.join('; ') : 'PASS  the upload toast follows the bytes sent, waits on the machine, says where it landed; a too-large file stays, a left session fades');
await b.close(); srv.close();
process.exit(fails.length ? 1 : 0);
