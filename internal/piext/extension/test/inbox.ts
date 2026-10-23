// inbox.ts against a stand-in for `reminal inbox-stream` (fake-inbox.mjs):
// who asks, what is passed on, what is acked, and when a stream is opened
// again or never.
//
// Run: node --experimental-strip-types test/inbox.ts

import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { EVENT, HELLO, startInbox, SUBSCRIBE } from "../inbox.ts";

const FAKE = path.join(path.dirname(fileURLToPath(import.meta.url)), "fake-inbox.mjs");

/** A pi with an event bus and lifecycle events: what inbox.ts uses. */
function fakePi() {
	const on = new Map<string, Function[]>();
	const bus = new Map<string, Function[]>();
	const add = (m: Map<string, Function[]>, k: string, f: Function) => m.set(k, [...(m.get(k) ?? []), f]);
	const pi = {
		on: (k: string, f: Function) => add(on, k, f),
		events: {
			on: (k: string, f: Function) => add(bus, k, f),
			emit: (k: string, d: unknown) => {
				for (const f of bus.get(k) ?? []) f(d);
			},
		},
	} as unknown as ExtensionAPI;
	const fire = (k: string) => {
		for (const f of on.get(k) ?? []) f({}, {});
	};
	return { pi, fire, bus: pi.events as unknown as { on: Function; emit: Function } };
}

/** Resolves when cond holds; the cap only ends a test that would hang. */
async function until(cond: () => boolean, what: string, capMs = 5000): Promise<void> {
	for (const end = Date.now() + capMs; Date.now() < end; await new Promise((r) => setTimeout(r, 20))) if (cond()) return;
	throw new Error("never: " + what);
}

function rig(mode: string, events: unknown[] = []) {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "inbox-"));
	fs.writeFileSync(path.join(dir, "mode"), mode);
	fs.writeFileSync(path.join(dir, "events"), events.map((e) => JSON.stringify(e)).join("\n") + "\n");
	process.env.FAKE_INBOX_DIR = dir;
	const read = (n: string) => (fs.existsSync(path.join(dir, n)) ? fs.readFileSync(path.join(dir, n), "utf8").split("\n").filter(Boolean) : []);
	return { dir, spawns: () => read("spawns"), acks: () => read("acks").map((l) => JSON.parse(l)), done: () => fs.rmSync(dir, { recursive: true, force: true }) };
}

const tests: [string, () => Promise<void>][] = [];
const test = (name: string, fn: () => Promise<void>) => tests.push([name, fn]);

test("outside a reminal session nothing is asked", async () => {
	const r = rig("events");
	delete process.env.REMINAL_SESSION;
	const { pi, bus, fire } = fakePi();
	startInbox(pi, FAKE);
	fire("session_start");
	bus.emit(SUBSCRIBE, { v: 1 });
	await new Promise((res) => setTimeout(res, 200));
	assert.deepEqual(r.spawns(), []);
	r.done();
});

test("hello at session_start, and nothing runs until someone subscribes", async () => {
	const r = rig("events");
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	let hellos = 0;
	bus.on(HELLO, () => hellos++);
	startInbox(pi, FAKE);
	fire("session_start");
	fire("agent_start");
	await new Promise((res) => setTimeout(res, 200));
	assert.equal(hellos, 1);
	assert.deepEqual(r.spawns(), []);
	bus.emit(SUBSCRIBE, { v: 2 }); // a version it does not speak
	await new Promise((res) => setTimeout(res, 200));
	assert.deepEqual(r.spawns(), []);
	r.done();
});

test("each event is passed on whole, acked once, and live while its stream is", async () => {
	const ev = { t: "event", v: 1, id: "m1", type: "message", urgency: "normal", body: "hi", line: "[x] hi" };
	const r = rig("events", [ev]);
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	const got: any[] = [];
	bus.on(EVENT, (d: any) => got.push(d));
	startInbox(pi, FAKE);
	bus.emit(SUBSCRIBE, { v: 1, name: "my-view" });
	await until(() => got.length === 1, "the event");
	assert.deepEqual(r.spawns(), ["inbox-stream 1 my-view"]);
	const { t: _t, ...want } = ev;
	assert.deepEqual(got[0].event, want);
	assert.equal(got[0].live(), true);
	got[0].ack("shown");
	got[0].ack("shown"); // twice: one ack
	got[0].ack("declined"); // after shown: none
	await until(() => r.acks().length >= 1, "the ack");
	await new Promise((res) => setTimeout(res, 200));
	assert.deepEqual(r.acks(), [{ t: "ack", id: "m1", status: "shown" }]);
	bus.emit(SUBSCRIBE, { v: 1 }); // subscribing twice is harmless: one stream
	await new Promise((res) => setTimeout(res, 200));
	assert.equal(r.spawns().length, 1);
	fire("session_shutdown");
	await until(() => !got[0].live(), "not live after shutdown");
	r.done();
});

test("a name that is not a plain word (a flag, a space, too long) is passed as pi", async () => {
	for (const bad of ["--help", "-x", "a b", "x".repeat(65), "naïve"]) {
		const r = rig("events");
		process.env.REMINAL_SESSION = "TESTSESS";
		const { pi, bus, fire } = fakePi();
		startInbox(pi, FAKE);
		bus.emit(SUBSCRIBE, { v: 1, name: bad });
		await until(() => r.spawns().length === 1, "the ask for " + JSON.stringify(bad));
		assert.deepEqual(r.spawns(), ["inbox-stream 1 pi"], "for " + JSON.stringify(bad));
		fire("session_shutdown");
		r.done();
	}
});

test("a stream reminal closed: its events no longer live, opened again at the next run", async () => {
	const ev = { t: "event", v: 1, id: "m2", type: "notice", body: "n", line: "[x] n" };
	const r = rig("close", [ev]);
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	const got: any[] = [];
	bus.on(EVENT, (d: any) => got.push(d));
	startInbox(pi, FAKE);
	bus.emit(SUBSCRIBE, { v: 1 });
	await until(() => got.length === 1, "the event");
	await until(() => !got[0].live(), "not live once closed");
	got[0].ack("shown"); // too late: reminal types it; nothing is sent
	assert.equal(r.spawns().length, 1);
	fire("agent_start");
	await until(() => r.spawns().length === 2, "opened again at the next run");
	fire("session_shutdown");
	r.done();
});

test("a reminal without the command is asked once, never again", async () => {
	const r = rig("absent");
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	startInbox(pi, FAKE);
	bus.emit(SUBSCRIBE, { v: 1 });
	await until(() => r.spawns().length === 1, "the one ask");
	await new Promise((res) => setTimeout(res, 300));
	fire("agent_start");
	fire("agent_end");
	bus.emit(SUBSCRIBE, { v: 1 });
	await new Promise((res) => setTimeout(res, 300));
	assert.equal(r.spawns().length, 1);
	r.done();
});

test("a reminal that failed without a word (not an unknown command) is asked again at the next run", async () => {
	const r = rig("broken");
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	startInbox(pi, FAKE);
	bus.emit(SUBSCRIBE, { v: 1 });
	await until(() => r.spawns().length === 1, "the first ask");
	await new Promise((res) => setTimeout(res, 300));
	assert.equal(r.spawns().length, 1, "not on a timer");
	fire("agent_start");
	await until(() => r.spawns().length === 2, "asked again at the next run");
	fire("session_shutdown");
	r.done();
});

test("no reminal to run at all: pi carries on, nothing thrown", async () => {
	const r = rig("events");
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	startInbox(pi, "/nonexistent/reminal");
	bus.emit(SUBSCRIBE, { v: 1 });
	await new Promise((res) => setTimeout(res, 300));
	fire("agent_start");
	bus.emit(SUBSCRIBE, { v: 1 });
	await new Promise((res) => setTimeout(res, 300));
	assert.deepEqual(r.spawns(), [], "nothing ran");
	fire("session_shutdown");
	r.done();
});

test("a refusal is asked again at the next run", async () => {
	const r = rig("refused");
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	startInbox(pi, FAKE);
	bus.emit(SUBSCRIBE, { v: 1 });
	await until(() => r.spawns().length === 1, "the first ask");
	await new Promise((res) => setTimeout(res, 300));
	fire("agent_start");
	await until(() => r.spawns().length === 2, "asked again");
	fire("session_shutdown");
	r.done();
});

test("replaced by another: never opened again", async () => {
	const r = rig("replaced");
	process.env.REMINAL_SESSION = "TESTSESS";
	const { pi, bus, fire } = fakePi();
	startInbox(pi, FAKE);
	bus.emit(SUBSCRIBE, { v: 1 });
	await until(() => r.spawns().length === 1, "the ask");
	await new Promise((res) => setTimeout(res, 300));
	fire("agent_start");
	await new Promise((res) => setTimeout(res, 300));
	assert.equal(r.spawns().length, 1);
	r.done();
});

let failed = 0;
for (const [name, fn] of tests) {
	try {
		await fn();
		console.log("ok   " + name);
	} catch (e) {
		failed++;
		console.log("FAIL " + name + ": " + (e as Error).message);
	}
}
console.log(failed ? `${failed} of ${tests.length} failed` : `all ${tests.length} passed`);
process.exit(failed ? 1 : 0);
