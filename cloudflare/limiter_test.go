// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package cloudflare

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The rate limiter (src/limiter.ts) counts: a caller past its limit is told
// no, everywhere at once (one object per caller); and a Worker deployed
// without its binding runs on, counting per isolate, saying so once. Run on
// the real source through node, which strips its types (22.6 and later).
func TestRateLimiterCountsAndRunsWithoutItsBinding(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	src, err := filepath.Abs(filepath.Join("src", "limiter.ts"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { RateLimiter, allow, inIsolate } from "file://` + src + `";
const out = [];
// One object per caller, as the binding hands them out.
const objects = new Map();
const env = { LIMITER: { idFromName: (n) => n, get: (n) => {
  if (!objects.has(n)) objects.set(n, new RateLimiter());
  return { fetch: (u) => objects.get(n).fetch(new Request(u)) };
} } };
const got = [];
for (let i = 0; i < 4; i++) got.push(await allow(env, "push", "203.0.113.9", 3, 60));
out.push("counted " + got.join(","));
out.push("another caller " + await allow(env, "push", "198.51.100.7", 3, 60));
const logs = [];
const log = console.log;
console.log = (...a) => logs.push(a.join(" "));
const a1 = await allow({}, "push", "x", 3, 60), a2 = await allow({}, "push", "y", 3, 60);
console.log = log;
out.push("unbound " + a1 + "," + a2 + " said " + logs.length);
const counts = new Map();
const local = [];
for (let i = 0; i < 4; i++) local.push(inIsolate(counts, "k", 3, 60));
out.push("in isolate " + local.join(","));
console.log(out.join("\n"));
`
	cmd := exec.Command(node, "--experimental-strip-types", "--no-warnings", "--input-type=module", "-e", script)
	b, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(b), "bad option") || strings.Contains(string(b), "Unknown file extension") {
			t.Skip("this node does not run TypeScript: " + strings.TrimSpace(string(b)))
		}
		t.Fatalf("%v: %s", err, b)
	}
	got := string(b)
	for _, want := range []string{
		"counted true,true,true,false", // its limit, then no
		"another caller true",          // one object per caller
		"unbound null,null said 1",     // no binding: not counted here (null), said once
		"in isolate true,true,true,false",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
}
