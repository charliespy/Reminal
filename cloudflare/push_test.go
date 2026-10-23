// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package cloudflare

import (
	"strings"
	"testing"
)

// Alerts are bound to the machine that registered them: the token reaches
// only the phone (sealed to its subscription), and an alert goes through
// only with that token and a fresh signature by the machine's key — once.
// Without a token: refused ("renew"), in words an older machine logs as they are.
func TestAlertsAreBoundToTheMachineThatRegisteredThem(t *testing.T) {
	got := runWorkerTS(t, `
import { handlePush, handlePushToken, alertSigned } from "SRC/push.ts";
const out = [];
const say = (k, v) => out.push(k + " " + v);
const enc = new TextEncoder(), dec = new TextDecoder();
const b64 = (u8) => btoa(String.fromCharCode(...new Uint8Array(u8)));
const b64u = (u8) => b64(u8).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const unb64 = (s) => Uint8Array.from(atob(s.replace(/-/g, "+").replace(/_/g, "/")), (c) => c.charCodeAt(0));
const hex = (buf) => [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, "0")).join("");

// The phone: its subscription's keys.
const ua = await crypto.subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
const p256dh = new Uint8Array(await crypto.subtle.exportKey("raw", ua.publicKey));
const auth = crypto.getRandomValues(new Uint8Array(16));
const endpoint = "https://fcm.googleapis.com/fcm/send/victim";
// What the phone reads of a message sealed to it (RFC 8291).
async function open(body) {
  const salt = body.slice(0, 16), idlen = body[20], asPub = body.slice(21, 21 + idlen), ct = body.slice(21 + idlen);
  const as = await crypto.subtle.importKey("raw", asPub, { name: "ECDH", namedCurve: "P-256" }, false, []);
  const ecdh = await crypto.subtle.deriveBits({ name: "ECDH", public: as }, ua.privateKey, 256);
  const hkdf = async (s, ikm, info, n) => crypto.subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: s, info }, await crypto.subtle.importKey("raw", ikm, "HKDF", false, ["deriveBits"]), n * 8);
  const cat = (...p) => { const o = new Uint8Array(p.reduce((n, x) => n + x.length, 0)); let i = 0; for (const x of p) { o.set(x, i); i += x.length; } return o; };
  const ikm = await hkdf(auth, ecdh, cat(enc.encode("WebPush: info\0"), p256dh, asPub), 32);
  const cek = await hkdf(salt, ikm, enc.encode("Content-Encoding: aes128gcm\0"), 16);
  const nonce = await hkdf(salt, ikm, enc.encode("Content-Encoding: nonce\0"), 12);
  const pt = new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv: nonce }, await crypto.subtle.importKey("raw", cek, "AES-GCM", false, ["decrypt"]), ct));
  if (pt[pt.length - 1] !== 2) throw new Error("no last-record delimiter");
  return JSON.parse(dec.decode(pt.slice(0, -1)));
}

const vapid = await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, true, ["sign"]);
const env = { VAPID_PUBLIC: "x", VAPID_PRIVATE_JWK: JSON.stringify(await crypto.subtle.exportKey("jwk", vapid.privateKey)), PUSH_TOKEN_KEY: "k".repeat(48) };
const sent = [];
globalThis.fetch = async (u, init) => { sent.push({ u: String(u), body: new Uint8Array(init.body) }); return new Response(null, { status: 201 }); };
let ip = 0;
const post = (path, body, now) => (path === "/push/token" ? handlePushToken : handlePush)(
  new Request("https://live.reminal.app" + path, { method: "POST", body: JSON.stringify(body), headers: { "cf-connecting-ip": "203.0.113." + (++ip % 250) } }), env, now);

// Two machines: the one alerts were turned on for, and another.
const mk = async () => { const k = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]); return { k, pub: b64(await crypto.subtle.exportKey("raw", k.publicKey)) }; };
const mine = await mk(), theirs = await mk();
const sub = { endpoint, p256dh: b64u(p256dh), auth: b64u(auth) };

// The token: asked by the page for this machine, sent to the phone.
let r = await post("/push/token", { ...sub, machine: mine.pub });
say("token-ask", r.status + " sent " + sent.length + " to " + sent[0].u);
const msg = await open(sent[0].body);
say("phone-reads", msg.kind + " for-machine " + (msg.machine === mine.pub) + " token " + /^v1\.\d+\.[A-Za-z0-9_-]{43}$/.test(msg.token));
const token = msg.token;

const payload = new Uint8Array(150).fill(7);
const alert = async (who, extra = {}, now = Date.now()) => {
  const ts = Math.floor(now / 1000);
  const signed = alertSigned(endpoint, hex(await crypto.subtle.digest("SHA-256", payload)), ts);
  const sig = b64(await crypto.subtle.sign({ name: "Ed25519" }, who.k.privateKey, enc.encode(signed)));
  return { endpoint, payload: b64(payload), token, machine: who.pub, ts, sig, ...extra };
};
const n0 = sent.length;
const good = await alert(mine);
r = await post("/push", good);
say("bound", r.status + " forwarded " + (sent.length - n0));
r = await post("/push", good);
say("replay", r.status);
// Another machine, with this subscription and its token: its own key.
r = await post("/push", await alert(theirs));
say("other-key", r.status + " " + (await r.json()).code);
// A token asked for another machine goes to the phone, not to whoever asked.
const n1 = sent.length;
r = await post("/push/token", { ...sub, machine: theirs.pub });
say("other-asks", r.status + " to-phone " + (sent.length - n1) + " " + (sent[sent.length - 1].u === endpoint));
// A token the relay did not make.
r = await post("/push", await alert(theirs, { token: "v1." + Math.floor(Date.now() / 1000) + "." + "A".repeat(43) }));
say("made-up-token", r.status);
r = await post("/push", await alert(mine, {}, Date.now() - 600 * 1000));
say("stale", r.status);
r = await post("/push", { ...(await alert(mine)), sig: b64(new Uint8Array(64)) });
say("bad-sig", r.status);
// Alerts that are not the machine's spend nothing of the subscription's allowance.
for (let i = 0; i < 30; i++) await post("/push", await alert(theirs));
r = await post("/push", await alert(mine, {}, Date.now() + 1000));
say("after-refused", r.status);
// Untokened: as an older machine sends.
const old = { endpoint, payload: b64(payload) };
r = await post("/push", old);
const ub = await r.json();
say("untokened", r.status + " " + ub.code + " " + /update reminal on this machine to keep phone alerts/.test(ub.error));
// Asking for tokens: three in ten minutes for one subscription (the two
// above count), then refused.
r = await post("/push/token", { ...sub, machine: mine.pub });
say("ask-3", r.status);
r = await post("/push/token", { ...sub, machine: mine.pub });
say("ask-4", r.status);
r = await post("/push/token", { ...sub, endpoint: "https://fcm.googleapis.com/fcm/send/other", machine: mine.pub });
say("ask-other", r.status);
r = await post("/push/token", { ...sub, machine: "short" });
say("ask-bad-machine", r.status);
// Another spelling of the same endpoint is not another subscription: refused,
// so it cannot get round the cap (each ask shows a notification on the phone).
const spellings = [endpoint + "#2", endpoint + "?a=1", endpoint.replace("fcm.", "FCM."), endpoint.replace(".com/", ".com:443/")];
const n2 = sent.length;
const sp = [];
for (const e of spellings) sp.push((await post("/push/token", { ...sub, endpoint: e, machine: mine.pub })).status);
for (const e of spellings) sp.push((await post("/push", { ...(await alert(mine)), endpoint: e })).status);
say("spellings", sp.join(",") + " sent " + (sent.length - n2));
// A relay without PUSH_TOKEN_KEY can check no token: it forwards nothing,
// and says it is not set up (not "unbound", which would make machines drop
// a good token).
const keyless = (body) => handlePush(new Request("https://live.reminal.app/push", { method: "POST", body: JSON.stringify(body), headers: { "cf-connecting-ip": "198.51.100.1" } }), { ...env, PUSH_TOKEN_KEY: undefined });
r = await keyless(await alert(mine));
const kb = r.status + " " + ((await r.json()).code ?? "-");
r = await keyless(old);
say("keyless", kb + " " + r.status + " " + ((await r.json()).code ?? "-"));
// A time off the relay's: refused with the relay's own, to set the clock by.
const now0 = Date.now();
r = await post("/push", await alert(mine, {}, now0 - 600 * 1000), now0);
const cb = await r.json();
say("clock", r.status + " " + cb.code + " " + (cb.now === now0));
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"token-ask 200 sent 1 to https://fcm.googleapis.com/fcm/send/victim",
		"phone-reads alerts-on for-machine true token true",
		"bound 200 forwarded 1",
		"replay 409",
		"other-key 403 unbound",
		"other-asks 200 to-phone 1 true",
		"made-up-token 403",
		"stale 403",
		"bad-sig 403",
		"after-refused 200",
		"untokened 403 renew true",
		"ask-3 200",
		"ask-4 429",
		"ask-other 200",
		"ask-bad-machine 400",
		"spellings 400,400,400,400,400,400,400,400 sent 0",
		"keyless 501 - 501 -",
		"clock 403 clock true",
	} {
		if !strings.Contains(got, want+"\n") && !strings.HasSuffix(strings.TrimSpace(got), want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

// A push is posted to the endpoint that was allowed and nowhere else: the
// post goes with redirect "manual", and a push service that answers with a
// redirect is not followed — the caller is told its status, the failure it
// is. An endpoint longer than any push service's is refused before anything
// is sent.
func TestAPushIsNeverSentOnElsewhere(t *testing.T) {
	got := runWorkerTS(t, `
import { handlePushToken, allowedEndpoint } from "SRC/push.ts";
const out = [];
const say = (k, v) => out.push(k + " " + v);
const b64 = (u8) => btoa(String.fromCharCode(...new Uint8Array(u8)));
const b64u = (u8) => b64(u8).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
const ua = await crypto.subtle.generateKey({ name: "ECDH", namedCurve: "P-256" }, true, ["deriveBits"]);
const p256dh = new Uint8Array(await crypto.subtle.exportKey("raw", ua.publicKey));
const auth = crypto.getRandomValues(new Uint8Array(16));
const vapid = await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, true, ["sign"]);
const env = { VAPID_PUBLIC: "x", VAPID_PRIVATE_JWK: JSON.stringify(await crypto.subtle.exportKey("jwk", vapid.privateKey)), PUSH_TOKEN_KEY: "k".repeat(48) };
const k = await crypto.subtle.generateKey({ name: "Ed25519" }, true, ["sign", "verify"]);
const machine = b64(await crypto.subtle.exportKey("raw", k.publicKey));
const sent = [];
let answer = 201;
globalThis.fetch = async (u, init) => {
  sent.push({ u: String(u), redirect: init.redirect });
  return new Response(null, { status: answer, headers: answer >= 300 && answer < 400 ? { Location: "https://evil.example/collect" } : {} });
};
let ip = 0;
const ask = (endpoint) => handlePushToken(new Request("https://live.reminal.app/push/token", { method: "POST",
  body: JSON.stringify({ endpoint, p256dh: b64u(p256dh), auth: b64u(auth), machine }),
  headers: { "cf-connecting-ip": "203.0.113." + (++ip % 250) } }), env);

let r = await ask("https://fcm.googleapis.com/fcm/send/ok");
say("delivered", r.status + " " + JSON.stringify(await r.json()) + " redirect=" + sent[0].redirect);
answer = 302;
r = await ask("https://fcm.googleapis.com/fcm/send/moved");
const moved = await r.json();
say("redirected", r.status + " status=" + moved.status + " error=" + JSON.stringify(moved.error ?? "") + " posts=" + sent.length + " to=" + sent[sent.length - 1].u + " redirect=" + sent[sent.length - 1].redirect);
const long = "https://fcm.googleapis.com/fcm/send/" + "a".repeat(1024);
const n = sent.length;
r = await ask(long);
say("over-long", r.status + " allowed=" + (allowedEndpoint(long) !== null) + " posts=" + (sent.length - n));
say("at-limit", "allowed=" + (allowedEndpoint("https://fcm.googleapis.com/fcm/send/" + "a".repeat(1024 - 36)) !== null));
// An endpoint carrying credentials is refused; the same endpoint plain is not.
const creds = ["https://u:p@fcm.googleapis.com/fcm/send/x", "https://u@fcm.googleapis.com/fcm/send/x", "https://:p@fcm.googleapis.com/fcm/send/x"];
say("credentials", creds.map((e) => allowedEndpoint(e) !== null).join(",") + " plain=" + (allowedEndpoint("https://fcm.googleapis.com/fcm/send/x") !== null));
const n2 = sent.length;
r = await ask(creds[0]);
say("credentials-asked", r.status + " posts=" + (sent.length - n2));
console.log(out.join("\n"));
`)
	for _, want := range []string{
		`delivered 200 {"status":201} redirect=manual`,
		`redirected 200 status=302 error="the push service redirected; not followed" posts=2 to=https://fcm.googleapis.com/fcm/send/moved redirect=manual`,
		"over-long 400 allowed=false posts=0",
		"at-limit allowed=true",
		"credentials false,false,false plain=true",
		"credentials-asked 400 posts=0",
	} {
		if !strings.Contains(got, want+"\n") && !strings.HasSuffix(strings.TrimSpace(got), want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}
