// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package cloudflare

import (
	"strings"
	"testing"
)

// A machine's socket that has not authenticated writes nothing to a session
// never used (any id can be named, and such a room holds nothing to clear),
// neither as it opens nor as it leaves. In a room that was used, its opening
// still arms the room's expiry, so it cannot keep that room alive.
func TestAnUnauthenticatedSocketWritesNothingToARoomNeverUsed(t *testing.T) {
	got := runWorkerTS(t, `
// What the platform gives a room, stood in for: a socket pair, a 101
// response (node's Response refuses one), and storage that counts writes.
globalThis.WebSocketRequestResponsePair = class { constructor() {} };
const sock = () => ({ att: null, readyState: 1, serializeAttachment(a) { this.att = a; }, deserializeAttachment() { return this.att; }, send() {}, close() {} });
globalThis.WebSocketPair = class { constructor() { this[0] = sock(); this[1] = sock(); } };
const R = globalThis.Response;
globalThis.Response = class extends R { constructor(b, i) { super(b, i && i.status === 101 ? { ...i, status: 200 } : i); } };
const { SessionRoom } = await import("SRC/session.ts");
function room(kept) {
  const m = new Map(Object.entries(kept));
  const writes = [];
  let alarm = null;
  const sockets = [];
  const storage = {
    async get(k) { return Array.isArray(k) ? new Map(k.filter((x) => m.has(x)).map((x) => [x, m.get(x)])) : m.get(k); },
    async put(k, v) { if (typeof k === "string") { writes.push("put " + k); m.set(k, v); } else for (const [a, b] of Object.entries(k)) { writes.push("put " + a); m.set(a, b); } },
    async delete(k) { writes.push("delete"); m.delete(k); },
    async deleteAll() { writes.push("deleteAll"); m.clear(); },
    async getAlarm() { return alarm; },
    async setAlarm(t) { writes.push("setAlarm"); alarm = t; },
    async deleteAlarm() { writes.push("deleteAlarm"); alarm = null; },
  };
  const state = { storage, setWebSocketAutoResponse() {}, getWebSockets: () => sockets, acceptWebSocket(s) { sockets.push(s); }, id: { toString: () => "room" } };
  return { r: new SessionRoom(state, {}), writes, sockets };
}
const out = [];
for (const role of ["agent", "tunnel"]) {
  const fresh = room({});
  const res = await fresh.r.fetch(new Request("https://do/ws/ABC123/" + role, { headers: { Upgrade: "websocket" } }));
  out.push(role + " fresh open " + res.status + " accepted " + fresh.sockets.length + " writes [" + fresh.writes.join(",") + "]");
  await fresh.r.webSocketClose(fresh.sockets[0], 1006, "");
  out.push(role + " fresh close writes [" + fresh.writes.join(",") + "]");
  const used = room({ ownerHash: "h", ownerUntil: Date.now() + 1e9 });
  await used.r.fetch(new Request("https://do/ws/ABC123/" + role, { headers: { Upgrade: "websocket" } }));
  out.push(role + " used open armed " + used.writes.includes("setAlarm"));
}
console.log(out.join("\n"));
`)
	for _, want := range []string{
		"agent fresh open 200 accepted 1 writes []", "agent fresh close writes []", "agent used open armed true",
		"tunnel fresh open 200 accepted 1 writes []", "tunnel fresh close writes []", "tunnel used open armed true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}
