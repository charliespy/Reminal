// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package viewerbuild

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const placeholder = `<script src="/assets/header.0000000000000000.js" integrity="sha384-AAAA" crossorigin="anonymous"></script>
  <script src="/assets/app.0000000000000000.js" integrity="sha384-AAAA" crossorigin="anonymous"></script>`

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, tr := range Trees {
		if err := os.MkdirAll(filepath.Join(root, tr), 0o755); err != nil {
			t.Fatal(err)
		}
		page := "<!doctype html><title>t</title>\n  " + placeholder + "\n"
		if err := os.WriteFile(filepath.Join(root, tr, "index.html"), []byte(page), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// setSource sets app.js to js, and header.js to a fixed one.
func setSource(t *testing.T, root, js string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, Source("app")), []byte(js), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Source("header")), []byte("fit()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestABuildNamesTheScriptByItsContentInBothCopies(t *testing.T) {
	root := tree(t)
	setSource(t, root, "console.log('one')\n")
	if _, err := Build(root, true); err != nil {
		t.Fatal(err)
	}
	js := []byte("console.log('one')\n")
	for _, tr := range Trees {
		if got := read(t, root, filepath.Join(tr, "assets", Name("app", js))); got != string(js) {
			t.Fatalf("%s: the asset is not the script: %q", tr, got)
		}
		if page := read(t, root, filepath.Join(tr, "index.html")); !strings.Contains(page, Tag("app", js)) || !strings.Contains(page, Tag("header", []byte("fit()\n"))) || strings.Contains(page, "0000000000000000") {
			t.Fatalf("%s: the page does not name its script:\n%s", tr, page)
		}
	}
	if read(t, root, "internal/client/web/index.html") != read(t, root, "cloudflare/public/index.html") {
		t.Fatal("the two copies of the page differ")
	}
	// Built again from the same source: nothing to change.
	if changed, err := Build(root, false); err != nil || len(changed) != 0 {
		t.Fatalf("an up-to-date tree reported %v (%v)", changed, err)
	}
}

func TestAStaleBuildIsReportedAndNothingWrittenInCheck(t *testing.T) {
	root := tree(t)
	setSource(t, root, "a\n")
	if _, err := Build(root, true); err != nil {
		t.Fatal(err)
	}
	before := read(t, root, "internal/client/web/index.html")
	setSource(t, root, "b\n") // edited, not rebuilt
	changed, err := Build(root, false)
	if err != nil || len(changed) == 0 {
		t.Fatalf("a stale build went unreported: %v (%v)", changed, err)
	}
	if read(t, root, "internal/client/web/index.html") != before {
		t.Fatal("check mode wrote the page")
	}
	if _, err := os.Stat(filepath.Join(root, "internal/client/web/assets", Name("app", []byte("b\n")))); err == nil {
		t.Fatal("check mode wrote an asset")
	}
}

func TestTheLastThreeBuildsAreKept(t *testing.T) {
	root := tree(t)
	var names []string
	for _, s := range []string{"1\n", "2\n", "3\n", "4\n"} {
		setSource(t, root, s)
		if _, err := Build(root, true); err != nil {
			t.Fatal(err)
		}
		names = append(names, Name("app", []byte(s)))
	}
	for _, tr := range Trees {
		dir := filepath.Join(root, tr, "assets")
		for i, n := range names {
			_, err := os.Stat(filepath.Join(dir, n))
			if kept := i >= len(names)-Keep; kept != (err == nil) {
				t.Fatalf("%s: build %d kept=%v, want %v", tr, i+1, err == nil, kept)
			}
		}
		if got := strings.Split(strings.TrimSpace(read(t, root, filepath.Join(tr, "assets", "builds.txt"))), "\n"); len(got) != Keep || !strings.Contains(got[0], names[3]) {
			t.Fatalf("%s: builds.txt %v", tr, got)
		}
		// The header script, the same in every build, stays.
		if _, err := os.Stat(filepath.Join(dir, Name("header", []byte("fit()\n")))); err != nil {
			t.Fatalf("%s: the header script was dropped: %v", tr, err)
		}
	}
}

func TestAPageMustHaveExactlyOneTag(t *testing.T) {
	root := tree(t)
	setSource(t, root, "x\n")
	p := filepath.Join(root, "cloudflare/public/index.html")
	if err := os.WriteFile(p, []byte("<!doctype html>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(root, true); err == nil {
		t.Fatal("a page with no tag for its script built")
	}
}

// The repository's own build is current — or, before the page's script has
// moved out of it, there is none to check.
func TestTheViewerBuildIsCurrent(t *testing.T) {
	changed, err := Build(filepath.Join("..", ".."), false)
	if errors.Is(err, ErrNoSource) {
		t.Skip("the page still carries its script inline")
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) > 0 {
		t.Fatalf("out of date — run `go run ./cmd/viewerbuild`: %v", changed)
	}
}
