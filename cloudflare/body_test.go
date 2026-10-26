// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package cloudflare

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runBody runs script against the real src/body.ts through node, which
// strips its types (22.6 and later), and returns what it printed.
func runBody(t *testing.T, script string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src, err := filepath.Abs(filepath.Join("src", "body.ts"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--experimental-strip-types", "--no-warnings", "--input-type=module", "-e",
		`import * as b from "file://`+src+`";`+"\n"+script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "bad option") || strings.Contains(string(out), "Unknown file extension") {
			t.Skip("this node does not run TypeScript: " + strings.TrimSpace(string(out)))
		}
		t.Fatalf("%v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// A long body (4 MB here) sent without its length, and how many of its
// 64 KiB chunks were read.
const longBody = `
function long() {
  let pulled = 0;
  const chunk = new Uint8Array(64 << 10).fill(0x61);
  const body = new ReadableStream({ pull(c) { if (pulled++ < 64) c.enqueue(chunk); else c.close(); } });
  return { body, pulled: () => pulled };
}
`

// A body sent without its length is held no further than the cap it is
// read to: its size is not known until it is read, and one read whole first
// is held whole, whatever is refused after. The rest is still read, and let
// go as it comes (an answer sent before the body is read leaves the Worker
// passing it on reading a request already answered).
func TestABodyIsHeldNoFurtherThanItsCap(t *testing.T) {
	got := runBody(t, longBody+`
const budget = new b.BodyBudget(1 << 30);
let peak = 0;
const take = budget.take.bind(budget);
budget.take = (n) => { const ok = take(n); peak = Math.max(peak, budget.held); return ok; };
const l = long();
const r = await b.readBody(l.body, 64 << 10, budget);
console.log(r + " " + (peak <= 64 << 10) + " " + budget.held + " " + l.pulled());
const s = long();
const whole = await b.readBody(s.body, 8 << 20);
console.log(whole.byteLength);
`)
	if want := "large true 0 65\n4194304"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Bodies read at once share one budget: each under its own cap, together
// they are held to what one may be — and what a refused one took is given
// back.
func TestBodiesReadAtOnceShareOneBudget(t *testing.T) {
	got := runBody(t, longBody+`
const budget = new b.BodyBudget(6 << 20);
const one = await b.readBody(long().body, 8 << 20, budget);
const two = await b.readBody(long().body, 8 << 20, budget);
console.log(one.byteLength + " " + two + " " + budget.held);
budget.give(one.byteLength);
const three = await b.readBody(long().body, 8 << 20, budget);
console.log(three.byteLength + " " + budget.held);
`)
	if want := "4194304 busy 4194304\n4194304 4194304"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The tunnel's room reads a request body only through readBody: the PIN
// form, which anyone may post, and an upload to the app behind it.
func TestTunnelRequestBodiesReadThroughTheirCap(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("src", "session.ts"))
	if err != nil {
		t.Fatal(err)
	}
	whole := regexp.MustCompile(`request\.(formData|arrayBuffer|text|json|blob)\(\)`)
	for i, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		if whole.MatchString(line) {
			t.Errorf("src/session.ts:%d reads a request body whole; use readBody: %s", i+1, strings.TrimSpace(line))
		}
	}
}
