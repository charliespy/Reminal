// Messages for this session, handed to another pi extension instead of typed.
//
// reminal types what arrives for a session into its terminal. A pi extension
// that would rather show those messages in places of its own asks for them on
// pi's event bus, and this file passes them on:
//
//   that extension → here   "reminal:inbox:subscribe"  { v: 1, name? }
//   here → that extension   "reminal:inbox:hello"      (ready: subscribe again, if you loaded first)
//   here → that extension   "reminal:inbox:event"      { event, ack(status), live() }
//
// Each event carries `line`, the exact text reminal would have typed. The
// extension puts it into the conversation, shows it where it likes, and then
// calls ack("shown"), or ack("declined") if it can't. Anything not acked in
// time, or acked "declined", reminal types into the terminal as it always
// does, so nothing is lost when no one is listening. `live()` turns false
// once the stream that carried an event is gone; an event not yet shown by
// then is reminal's to type, and the extension drops it.
//
// The messages come from `reminal inbox-stream 1 <name>`, one JSON object a
// line each way. A reminal without that command exits at once, and this stays
// out of the way. Nothing here runs outside a reminal session, or until an
// extension subscribes.

import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export const SUBSCRIBE = "reminal:inbox:subscribe";
export const HELLO = "reminal:inbox:hello";
export const EVENT = "reminal:inbox:event";

type Status = "shown" | "declined";

// What a subscriber may call itself (it is passed to reminal as an argument).
const NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

export function startInbox(pi: ExtensionAPI, bin: string): void {
	// Outside a reminal session there is nothing to stream; a pi without an
	// event bus has no one to hand it to.
	if ((process.env.REMINAL_SESSION ?? "") === "" || !pi.events) return;

	let wanted = false;
	let name = "pi";
	let proc: ChildProcessWithoutNullStreams | undefined;
	// A reminal that has no inbox stream at all: asked once, never again.
	let unavailable = false;
	// Another extension in this session took the stream over: it is theirs.
	let replaced = false;

	const open = (): void => {
		if (!wanted || proc || unavailable || replaced) return;
		let p: ChildProcessWithoutNullStreams;
		try {
			p = spawn(bin, ["inbox-stream", "1", name], { stdio: ["pipe", "pipe", "ignore"] });
		} catch {
			unavailable = true;
			return;
		}
		proc = p;
		// Anything said at all: a reminal that has the command (ready, or a
		// refusal it may lift by the next run).
		let said = false;
		let buf = "";
		const live = (): boolean => proc === p;
		const send = (v: unknown): void => {
			if (!live() || !p.stdin.writable) return;
			try {
				p.stdin.write(JSON.stringify(v) + "\n");
			} catch {
				// gone: what was not acked is reminal's to type
			}
		};
		const onLine = (line: string): void => {
			let m: Record<string, unknown>;
			try {
				m = JSON.parse(line);
			} catch {
				return;
			}
			switch (m.t) {
				case "ready":
					return;
				case "replaced":
					replaced = true;
					return;
				case "event": {
					const { t: _t, ...event } = m;
					let acked = false;
					const ack = (status: Status): void => {
						if (acked || (status !== "shown" && status !== "declined")) return;
						acked = true;
						send({ t: "ack", id: event.id, status });
					};
					pi.events.emit(EVENT, { event, ack, live });
					return;
				}
			}
		};
		p.stdout.setEncoding("utf8");
		p.stdout.on("data", (chunk: string) => {
			said = true;
			buf += chunk;
			for (let i = buf.indexOf("\n"); i >= 0; i = buf.indexOf("\n")) {
				const line = buf.slice(0, i);
				buf = buf.slice(i + 1);
				if (line.trim()) onLine(line);
			}
		});
		p.stdin.on("error", () => {});
		p.on("error", () => {
			unavailable = true;
			if (proc === p) proc = undefined;
		});
		p.on("exit", () => {
			if (proc === p) proc = undefined;
			// Not a word: this reminal has no inbox stream. A refusal is asked
			// again at the next run; an absent command never.
			if (!said) unavailable = true;
		});
	};

	// Asked for, by whichever extension wants the messages.
	pi.events.on(SUBSCRIBE, (data: unknown) => {
		const d = (data ?? {}) as { v?: unknown; name?: unknown };
		if (d.v !== 1) return;
		// A name reminal is handed as an argument: letters, digits, '.', '_'
		// and '-', not leading with '-' (never read as a flag). Anything else
		// keeps "pi".
		if (typeof d.name === "string" && NAME.test(d.name.trim())) name = d.name.trim();
		wanted = true;
		open();
	});

	// Ready: an extension loaded before this one subscribes again now.
	pi.on("session_start", () => {
		pi.events.emit(HELLO, { v: 1 });
	});

	// A stream reminal closed (an event not acked in time) is opened again at
	// the next turn, as pi starts or ends one: never on a timer.
	for (const ev of ["agent_start", "turn_start", "agent_end"] as const) pi.on(ev, () => open());

	pi.on("session_shutdown", () => {
		wanted = false;
		const p = proc;
		proc = undefined;
		try {
			p?.kill();
		} catch {
			// already gone
		}
	});
}
