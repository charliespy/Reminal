#!/usr/bin/env node
// A stand-in for `reminal inbox-stream 1 <name>`, for test/inbox.ts.
//
// $FAKE_INBOX_DIR/spawns gets one line per run (its arguments), and
// $FAKE_INBOX_DIR/acks every line it is sent. What it says is up to
// $FAKE_INBOX_DIR/mode, read as it starts:
//   absent     a reminal without the command: a word on stderr, exit 2
//   refused    {"t":"refused"} and exit 1
//   events     ready, then each line of $FAKE_INBOX_DIR/events, then it waits
//              for stdin to close
//   close      ready, one event, then it ends (reminal closing the stream)
//   replaced   ready, then {"t":"replaced"}, then it ends
import * as fs from "node:fs";
import * as path from "node:path";

const dir = process.env.FAKE_INBOX_DIR;
const file = (n) => path.join(dir, n);
fs.appendFileSync(file("spawns"), process.argv.slice(2).join(" ") + "\n");
if (process.argv[2] !== "inbox-stream") process.exit(2);
const mode = fs.readFileSync(file("mode"), "utf8").trim();
const out = (v) => process.stdout.write(JSON.stringify(v) + "\n");
const events = () => fs.readFileSync(file("events"), "utf8").split("\n").filter(Boolean);
process.stdin.setEncoding("utf8");
process.stdin.on("data", (d) => fs.appendFileSync(file("acks"), d));
switch (mode) {
	case "absent":
		process.stderr.write('reminal: unknown command "inbox-stream"\n');
		process.exit(2);
	case "refused":
		out({ t: "refused", why: "no" });
		process.exit(1);
	case "events":
		out({ t: "ready", v: 1 });
		for (const l of events()) process.stdout.write(l + "\n");
		process.stdin.on("end", () => process.exit(0));
		break;
	case "close":
		out({ t: "ready", v: 1 });
		process.stdout.write(events()[0] + "\n", () => process.exit(0));
		break;
	case "replaced":
		out({ t: "ready", v: 1 });
		out({ t: "replaced" });
		process.exit(0);
}
