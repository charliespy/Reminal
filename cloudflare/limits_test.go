// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package cloudflare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runWorkerTS runs script (an ES module) with the Worker's real sources: node
// strips their types (22.6 and later), and a resolve hook lets their
// extensionless imports ("./session") find the .ts files, so nothing has to
// be installed or bundled first.
func runWorkerTS(t *testing.T, script string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks.mjs")
	if err := os.WriteFile(hooks, []byte(`
export async function resolve(spec, ctx, next) {
  if (spec.startsWith("./") && !/\.(ts|js|mjs)$/.test(spec)) {
    try { return await next(spec + ".ts", ctx); } catch {}
  }
  return next(spec, ctx);
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := filepath.Join(dir, "register.mjs")
	if err := os.WriteFile(reg, []byte(`import { register } from "node:module"; register("file://`+hooks+`");`), 0o644); err != nil {
		t.Fatal(err)
	}
	src, err := filepath.Abs("src")
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mjs")
	if err := os.WriteFile(main, []byte(strings.ReplaceAll(script, "SRC", "file://"+src)), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := exec.Command(node, "--experimental-strip-types", "--no-warnings", "--import", "file://"+reg, main).CombinedOutput()
	if err != nil {
		if strings.Contains(string(b), "bad option") || strings.Contains(string(b), "Unknown file extension") {
			t.Skip("this node does not run TypeScript: " + strings.TrimSpace(string(b)))
		}
		t.Fatalf("%v: %s", err, b)
	}
	return string(b)
}

// Before any session's or rendezvous's object is touched, a caller's sockets
// (/ws, /rv) are counted per caller, by the rate limiter: past the count,
// 429, and no object asked. A machine's own sockets (/ws agent, tunnel) are
// counted apart, with room for the many machines one address can be.
func TestTheRelayCountsACallerBeforeItTouchesAnyObject(t *testing.T) {
	got := runWorkerTS(t, `
import worker from "SRC/index.ts";
const out = [];
function env(deny) {
  const seen = [];
  return { seen, e: {
    SESSION: { idFromName: (n) => n, get: (n) => ({ fetch: async () => { seen.push("session:" + n); return new Response("ok"); } }) },
    RENDEZVOUS: { idFromName: (n) => n, get: (n) => ({ fetch: async () => { seen.push("rv:" + n); return new Response("ok"); } }) },
    LIMITER: { idFromName: (n) => n, get: (n) => ({ fetch: async () => { seen.push("lim:" + n); return new Response(JSON.stringify({ success: !deny.some((d) => n.startsWith(d)) })); } }) },
  } };
}
const req = (path, host = "live.reminal.app") => new Request("https://" + host + path, { headers: { host, "cf-connecting-ip": "2001:db8:1:2:aa:bb:cc:1" } });
for (const [name, path, host, scope] of [
  ["ws", "/ws/ABC123/viewer", undefined, "sockets:"],
  ["rv", "/rv/CODE12/source", undefined, "sockets:"],
  ["agent", "/ws/ABC123/agent", undefined, "agent-sockets:"],
  ["tunnel", "/ws/ABC123/tunnel", undefined, "agent-sockets:"],
]) {
  const no = env([scope]);
  const r = await worker.fetch(req(path, host), no.e);
  const touched = no.seen.filter((s) => !s.startsWith("lim:"));
  out.push(name + " denied " + r.status + " touched " + touched.length);
  const yes = env([]);
  await worker.fetch(req(path, host), yes.e);
  out.push(name + " allowed asked " + yes.seen.filter((s) => s.startsWith("lim:")).join(",") + " touched " + yes.seen.filter((s) => !s.startsWith("lim:")).length);
}
// one address, many machines (a NAT, a cloud's way out): with no limiter
// bound, the isolate's own count: 120 viewers' sockets a minute, and the
// machines' own sockets counted apart, 1200
const solo = { SESSION: { idFromName: (n) => n, get: () => ({ fetch: async () => new Response("ok") }) } };
const ip = (path) => new Request("https://live.reminal.app" + path, { headers: { "cf-connecting-ip": "198.51.100.7" } });
for (const [name, path, n] of [["viewers", "/ws/ABC123/viewer", 121], ["agents", "/ws/ABC123/agent", 1201]]) {
  let ok = 0, last = 0;
  for (let i = 0; i < n; i++) { last = (await worker.fetch(ip(path), solo)).status; if (last === 200) ok++; }
  out.push("one address " + name + " " + ok + " ok then " + last);
}
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"ws denied 429 touched 0", "ws allowed asked lim:sockets:2001:db8:1:2::/64 touched 1",
		"rv denied 429 touched 0", "rv allowed asked lim:sockets:2001:db8:1:2::/64 touched 1",
		"agent denied 429 touched 0", "agent allowed asked lim:agent-sockets:2001:db8:1:2::/64 touched 1",
		"tunnel denied 429 touched 0", "tunnel allowed asked lim:agent-sockets:2001:db8:1:2::/64 touched 1",
		"one address viewers 120 ok then 429", "one address agents 1200 ok then 429",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

// A forwarded app's requests (/p, its own host) are counted in the isolate,
// each, and by the rate limiter once a batch of twenty: a page of fifty
// assets asks it three times, not fifty. Its no stands for the whole batch,
// with no object touched; and an isolate alone (no limiter) stops a caller
// at 1200 a minute.
func TestAForwardedAppsRequestsAskTheLimiterOnceABatch(t *testing.T) {
	got := runWorkerTS(t, `
import worker from "SRC/index.ts";
const out = [];
function env(deny, bound = true) {
  const seen = [];
  const e = {
    SESSION: { idFromName: (n) => n, get: (n) => ({ fetch: async () => { seen.push("session"); return new Response("ok"); } }) },
  };
  if (bound) e.LIMITER = { idFromName: (n) => n, get: (n) => ({ fetch: async () => { seen.push("lim:" + n); return new Response(JSON.stringify({ success: !deny })); } }) };
  return { seen, e, n: (p) => seen.filter((s) => s.startsWith(p)).length };
}
const req = (ip, path, host = "live.reminal.app") => new Request("https://" + host + path, { headers: { host, "cf-connecting-ip": ip } });
// a page and its assets, at once
const page = env(false);
const rs = await Promise.all(Array.from({ length: 50 }, (_, i) => worker.fetch(req("2001:db8:a::1", "/p/ABC123/a" + i), page.e)));
out.push("page " + rs.filter((r) => r.status === 200).length + " ok, limiter asked " + page.n("lim:tunnel:") + ", sessions " + page.n("session"));
// a batch refused: the first request and the 19 after it, no object touched, one ask
const no = env(true);
const st = [];
for (let i = 0; i < 20; i++) st.push((await worker.fetch(req("2001:db8:b::1", "/p/ABC123/"), no.e)).status);
out.push("refused " + st.filter((s) => s === 429).length + "/20, limiter asked " + no.n("lim:") + ", sessions " + no.n("session"));
await worker.fetch(req("2001:db8:b::1", "/p/ABC123/"), no.e);
out.push("next batch asks again: " + no.n("lim:"));
// a forwarded port's own host, the same
const port = env(true);
const pr = await worker.fetch(req("2001:db8:c::1", "/", "port-abc123.reminal.app"), port.e);
out.push("port " + pr.status + " asked " + port.n("lim:tunnel:") + " sessions " + port.n("session"));
// no limiter bound: the isolate's own count, 1200 a minute
const solo = env(false, false);
let ok = 0, last = 0;
for (let i = 0; i < 1201; i++) { last = (await worker.fetch(req("2001:db8:d::1", "/p/ABC123/"), solo.e)).status; if (last === 200) ok++; }
out.push("unbound " + ok + " ok then " + last);
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"page 50 ok, limiter asked 3, sessions 50",
		"refused 20/20, limiter asked 1, sessions 0",
		"next batch asks again: 2",
		"port 429 asked 1 sessions 0",
		"unbound 1200 ok then 429",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

// An alert is counted per caller and per subscription; past either, 429
// before anything is signed or sent. Nothing counts per push service (one
// count for every caller), and a caller (an address: many machines behind
// one NAT) has room for 600 a minute.
func TestAnAlertIsCountedPerCallerAndSubscription(t *testing.T) {
	got := runWorkerTS(t, `
import { handlePush } from "SRC/push.ts";
const out = [];
const kp = await crypto.subtle.generateKey({ name: "ECDSA", namedCurve: "P-256" }, true, ["sign"]);
const jwk = JSON.stringify(await crypto.subtle.exportKey("jwk", kp.privateKey));
const payload = btoa(String.fromCharCode(...new Uint8Array(120)));
const body = JSON.stringify({ endpoint: "https://fcm.googleapis.com/fcm/send/abc", payload });
const sent = [];
globalThis.fetch = async (u) => { sent.push(String(u)); return new Response(null, { status: 201 }); };
for (const deny of ["push-caller:", "push-endpoint:", "none"]) {
  const asked = [];
  const env = { VAPID_PUBLIC: "x", VAPID_PRIVATE_JWK: jwk, LIMITER: { idFromName: (n) => n, get: (n) => ({ fetch: async () => { asked.push(n); return new Response(JSON.stringify({ success: !n.startsWith(deny) })); } }) } };
  const r = await handlePush(new Request("https://live.reminal.app/push", { method: "POST", body, headers: { "cf-connecting-ip": "203.0.113.9" } }), env);
  out.push(deny + " " + r.status + " sent " + sent.length + " asked " + asked.map((a) => a.split(":")[0]).join(","));
}
// no limiter bound: one address, 600 alerts a minute to as many subscriptions
const solo = { VAPID_PUBLIC: "x", VAPID_PRIVATE_JWK: jwk };
let ok = 0, last = 0;
for (let i = 0; i < 601; i++) {
  const b = JSON.stringify({ endpoint: "https://fcm.googleapis.com/fcm/send/s" + i, payload });
  last = (await handlePush(new Request("https://live.reminal.app/push", { method: "POST", body: b, headers: { "cf-connecting-ip": "198.51.100.8" } }), solo)).status;
  if (last !== 429) ok++;
}
out.push("one address " + ok + " then " + last);
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"push-caller: 429 sent 0 asked push-caller",
		"push-endpoint: 429 sent 0 asked push-caller,push-endpoint",
		"none 200 sent 1 asked push-caller,push-endpoint\n",
		"one address 600 then 429",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

// A session takes so many viewer sockets at once (open or kept for a switch
// back); one more is refused before it is accepted.
func TestASessionTakesSoManyViewersAndRefusesTheNext(t *testing.T) {
	got := runWorkerTS(t, `
globalThis.WebSocketRequestResponsePair = class { constructor() {} };
const { SessionRoom, MAX_VIEWER_SOCKETS } = await import("SRC/session.ts");
const viewers = Array.from({ length: MAX_VIEWER_SOCKETS }, () => ({ deserializeAttachment: () => ({ role: "viewer" }) }));
let accepted = 0;
const state = { setWebSocketAutoResponse() {}, getWebSockets: () => viewers, acceptWebSocket() { accepted++; },
  storage: { get: async () => undefined, put: async () => {}, getAlarm: async () => null, setAlarm: async () => {} } };
const room = new SessionRoom(state, {});
const r = await room.fetch(new Request("https://do/ws/ABC123/viewer", { headers: { Upgrade: "websocket" } }));
console.log("cap " + MAX_VIEWER_SOCKETS + " next " + r.status + " accepted " + accepted);
`)
	if !strings.Contains(got, "cap 64 next 503 accepted 0") {
		t.Errorf("the next viewer past the cap: %s", got)
	}
}
