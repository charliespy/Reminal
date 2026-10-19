// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package cloudflare

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGate runs script against the real src/gate.ts through node, which strips
// its types (22.6 and later), and returns what it printed.
func runGate(t *testing.T, script string) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src, err := filepath.Abs(filepath.Join("src", "gate.ts"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--experimental-strip-types", "--no-warnings", "--input-type=module", "-e",
		`import * as g from "file://`+src+`";`+"\n"+script)
	b, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(b), "bad option") || strings.Contains(string(b), "Unknown file extension") {
			t.Skip("this node does not run TypeScript: " + strings.TrimSpace(string(b)))
		}
		t.Fatalf("%v: %s", err, b)
	}
	return strings.TrimSpace(string(b))
}

// A source that keeps getting the PIN wrong waits longer each time: five
// wrong PINs, then 5 minutes, then twice that, up to a day.
func TestGateWaitDoublesUpToADay(t *testing.T) {
	got := runGate(t, `
const min = 60000;
let st = { sources: {}, known: {} }, now = 0;
const waits = [];
for (let round = 0; round < 12; round++) {
  for (let i = 0; i < g.GATE_SOURCE_FREE; i++) {
    if (g.gateWait(st, "A", now) !== 0) { console.log("held early in round " + round + " at " + i); process.exit(); }
    st = g.gateWrong(st, "A", now);
  }
  const w = g.gateWait(st, "A", now);
  waits.push(w / min);
  now += w;
}
console.log(waits.join(","));
`)
	if want := "5,10,20,40,80,160,320,640,1280,1440,1440,1440"; got != want {
		t.Fatalf("waits in minutes: got %s, want %s", got, want)
	}
}

// A source quiet, after its last wait, for as long as that wait is forgiven;
// one that comes back sooner is not.
func TestGateForgivesAQuietSource(t *testing.T) {
	got := runGate(t, `
const wrong = (st, n, now) => { for (let i = 0; i < n; i++) st = g.gateWrong(st, "A", now); return st; };
let st = wrong({ sources: {}, known: {} }, 10, 0); // two waits: 5 min, then 10
const end = g.gateWait(st, "A", 0);
const soon = wrong(st, 5, end + 1);
const late = wrong(st, 4, end + 10 * 60000);
console.log((g.gateWait(soon, "A", end + 1) / 60000) + " " + g.gateWait(late, "A", end + 10 * 60000));
`)
	if got != "20 0" {
		t.Fatalf("got %q, want the third wait (20) when back soon and none when forgiven", got)
	}
}

// Strangers guessing from many addresses make every new source wait, but not
// one that has got the PIN right before, and not other sources over their own
// few wrong PINs before the tunnel's count is reached.
func TestGateStrangersCannotLockOutAKnownSource(t *testing.T) {
	got := runGate(t, `
let st = { sources: {}, known: {} }, now = 0;
st = g.gateRight(st, "owner", now);
st = g.gateWrong(st, "owner", now); // a typo of its own
let held = -1;
for (let i = 0; i < 400 && held < 0; i++) {
  const src = "s" + i;
  for (let j = 0; j < 4; j++) st = g.gateWrong(st, src, now);
  if (g.gateWait(st, "newcomer", now) > 0) held = i;
}
console.log("newcomer held after " + held + " sources; owner waits " + g.gateWait(st, "owner", now) +
  "; tunnel " + (g.gateWait(st, "newcomer", now) / 60000) + "; with a long PIN " + g.gateWait(st, "newcomer", now, false));
`)
	if want := "newcomer held after 12 sources; owner waits 0; tunnel 5; with a long PIN 0"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// What a tunnel keeps of its sources is bounded, and so is what it remembers
// of who got in.
func TestGateKeepsBoundedState(t *testing.T) {
	got := runGate(t, `
let st = { sources: {}, known: {} };
for (let i = 0; i < 2000; i++) st = g.gateWrong(st, "s" + i, i);
for (let i = 0; i < 200; i++) st = g.gateRight(st, "k" + i, i);
st = g.gateRight(st, g.NO_SOURCE, 300);
console.log(Object.keys(st.sources).length + " " + Object.keys(st.known).length + " " + ("k199" in st.known) + " " + ("k0" in st.known) + " " + (g.NO_SOURCE in st.known));
`)
	if want := "512 64 true false false"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A typed PIN is read as issued: case, spaces and dashes do not matter, and
// the letters base32 leaves out read as the digits they look like.
func TestGateReadsATypedPIN(t *testing.T) {
	got := runGate(t, `
console.log(["abcd-efgh jk1o", "123456", " 12 34 56 ", "il0O", "7K3M9QX2TZ4B"].map(g.normalizePIN).join(","));
`)
	if want := "ABCDEFGHJK10,123456,123456,1100,7K3M9QX2TZ4B"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Each visitor gets a cookie of its own, which lets it in for a month and no
// longer, under that tunnel's key only, and cannot be altered.
func TestGateVisitorCookie(t *testing.T) {
	got := runGate(t, `
const key = "11".repeat(32), other = "22".repeat(32), now = 1_800_000_000_000, day = 86400000;
const a = await g.visitorCookie(key, now), b = await g.visitorCookie(key, now);
const flip = (s, i) => s.slice(0, i) + (s[i] === "0" ? "1" : "0") + s.slice(i + 1);
const out = [
  a !== b,
  await g.visitorCookieOK(key, a, now),
  await g.visitorCookieOK(key, a, now + 29 * day),
  await g.visitorCookieOK(key, a, now + 31 * day),
  await g.visitorCookieOK(other, a, now),
  await g.visitorCookieOK(key, flip(a, 3), now),
  await g.visitorCookieOK(key, a.replace(/\.(\d+)\./, (_, t) => "." + (Number(t) + 86400 * 20) + "."), now + 40 * day),
  await g.visitorCookieOK(key, await g.legacyCookie(key), now),
  await g.visitorCookieOK(key, "", now),
];
console.log(out.join(","));
`)
	if want := "true,true,true,false,false,false,false,false,false"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
