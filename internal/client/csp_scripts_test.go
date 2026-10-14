// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The CSP names each CDN script by its exact URL, not the CDN: every script
// URL the page and its program load from a CDN is in it, and nothing else is.
func TestCSPNamesEveryCDNScriptThePageLoads(t *testing.T) {
	var used []string
	seen := map[string]bool{}
	scriptURL := regexp.MustCompile(`https://(?:cdn\.jsdelivr\.net|cdnjs\.cloudflare\.com)/[^"'\s)]+\.js`)
	for _, f := range []string{"web/index.html", "web/app.js", "web/header.js"} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range scriptURL.FindAllString(string(b), -1) {
			if !seen[u] {
				seen[u] = true
				used = append(used, u)
			}
		}
	}
	var named []string
	for _, f := range strings.Fields(viewerCSP[strings.Index(viewerCSP, "script-src"):]) {
		if strings.HasPrefix(f, "https://") {
			named = append(named, strings.TrimSuffix(f, ";"))
		}
	}
	sort.Strings(used)
	sort.Strings(named)
	if strings.Join(used, " ") != strings.Join(named, " ") {
		t.Fatalf("the CSP's scripts and the page's differ:\n page: %v\n CSP:  %v", used, named)
	}
	if strings.Contains(viewerCSP, "https://cdn.jsdelivr.net ") || strings.Contains(viewerCSP, "https://cdnjs.cloudflare.com ") {
		t.Fatal("the CSP allows a whole CDN")
	}
}

// viewerText is the viewer as a browser runs it: the page in dir, then each
// script it loads from its own assets/, in the page's order. What the static
// checks look for in the page's code is in those scripts now, not the page.
func viewerText(t *testing.T, dir string) string {
	t.Helper()
	page, err := os.ReadFile(dir + "/index.html")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.Write(page)
	srcs := regexp.MustCompile(`<script src="/(assets/[^"/]+\.js)"`).FindAllStringSubmatch(string(page), -1)
	if len(srcs) == 0 {
		t.Fatalf("%s/index.html loads no script of its own from assets/", dir)
	}
	for _, m := range srcs {
		js, err := os.ReadFile(dir + "/" + m[1])
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString("\n")
		b.Write(js)
	}
	return b.String()
}
