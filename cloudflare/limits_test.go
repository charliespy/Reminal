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
// (/ws, /rv) and a forwarded app's requests (/p, its own host) are counted
// per caller, by the rate limiter: past the count, 429, and no object asked.
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
  ["p", "/p/ABC123/", undefined, "tunnel:"],
  ["port", "/", "port-abc123.reminal.app", "tunnel:"],
]) {
  const no = env([scope]);
  const r = await worker.fetch(req(path, host), no.e);
  const touched = no.seen.filter((s) => !s.startsWith("lim:"));
  out.push(name + " denied " + r.status + " touched " + touched.length);
  const yes = env([]);
  await worker.fetch(req(path, host), yes.e);
  out.push(name + " allowed asked " + yes.seen.filter((s) => s.startsWith("lim:")).join(",") + " touched " + yes.seen.filter((s) => !s.startsWith("lim:")).length);
}
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"ws denied 429 touched 0", "ws allowed asked lim:sockets:2001:db8:1:2::/64 touched 1",
		"rv denied 429 touched 0", "rv allowed asked lim:sockets:2001:db8:1:2::/64 touched 1",
		"p denied 429 touched 0", "p allowed asked lim:tunnel:2001:db8:1:2::/64 touched 1",
		"port denied 429 touched 0", "port allowed asked lim:tunnel:2001:db8:1:2::/64 touched 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}

// An alert is counted per caller, per subscription and per push service; past
// any, 429 before anything is signed or sent.
func TestAnAlertIsCountedPerCallerSubscriptionAndService(t *testing.T) {
	got := runWorkerTS(t, `
import { handlePush } from "SRC/push.ts";
const out = [];
const payload = btoa(String.fromCharCode(...new Uint8Array(120)));
const body = JSON.stringify({ endpoint: "https://fcm.googleapis.com/fcm/send/abc", payload });
const sent = [];
globalThis.fetch = async (u) => { sent.push(String(u)); return new Response(null, { status: 201 }); };
for (const deny of ["push-caller:", "push-endpoint:", "push-service:"]) {
  const asked = [];
  const env = { VAPID_PUBLIC: "x", VAPID_PRIVATE_JWK: "{}", LIMITER: { idFromName: (n) => n, get: (n) => ({ fetch: async () => { asked.push(n); return new Response(JSON.stringify({ success: !n.startsWith(deny) })); } }) } };
  const r = await handlePush(new Request("https://live.reminal.app/push", { method: "POST", body, headers: { "cf-connecting-ip": "203.0.113.9" } }), env);
  out.push(deny + " " + r.status + " sent " + sent.length + " asked " + asked.map((a) => a.split(":")[0]).join(","));
}
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"push-caller: 429 sent 0 asked push-caller",
		"push-endpoint: 429 sent 0 asked push-caller,push-endpoint",
		"push-service: 429 sent 0 asked push-caller,push-endpoint,push-service",
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
