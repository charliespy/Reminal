// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

// Package viewerbuild makes the viewer's scripts cacheable: each source,
// internal/client/web/<name>.js, is written to assets/<name>.<hash>.js
// beside both copies of the page (internal/client/web, which the binary
// embeds, and cloudflare/public, which the Worker serves), and the page's
// one script tag for it is stamped with that name and its SRI hash. The name is
// the content's, so the file can be cached for good: a page names exactly
// the script it was built with, and a new script is a new name.
//
// The last Keep builds stay in assets/ (assets/builds.txt, newest first),
// so a page still loading from just before a deploy finds its own script.
// Run by `go run ./cmd/viewerbuild`; the tree is checked by a test that
// runs it with write false.
package viewerbuild

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Keep is how many builds' scripts stay served.
const Keep = 3

// Scripts are the page's scripts by name: <name>.js beside the page is the
// source of assets/<name>.<hash>.js. app is the page's program; header
// fits the header before the page is first drawn, so it loads where it was.
var Scripts = []string{"header", "app"}

// Source is a script's source, relative to the repository root.
func Source(name string) string { return "internal/client/web/" + name + ".js" }

// Trees are the page's copies, relative to the repository root; each gets
// an assets/ of its own, the same.
var Trees = []string{"internal/client/web", "cloudflare/public"}

// tagRE matches the page's tag for a script, whatever build it names.
func tagRE(script string) *regexp.Regexp {
	return regexp.MustCompile(`<script src="/assets/` + regexp.QuoteMeta(script) + `\.[0-9a-f]{16}\.js" integrity="sha384-[A-Za-z0-9+/=]+" crossorigin="anonymous"></script>`)
}

// ErrNoSource is returned when there is no app.js: the page still carries
// its script inline, and there is nothing to build.
var ErrNoSource = errors.New("viewerbuild: no " + Source("app"))

// Name is the asset name of a script's build.
func Name(script string, js []byte) string {
	sum := sha256.Sum256(js)
	return script + "." + hex.EncodeToString(sum[:])[:16] + ".js"
}

// SRI is the integrity value of a script.
func SRI(js []byte) string {
	sum := sha512.Sum384(js)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// Tag is the page's script tag for a script's build.
func Tag(script string, js []byte) string {
	return `<script src="/assets/` + Name(script, js) + `" integrity="` + SRI(js) + `" crossorigin="anonymous"></script>`
}

// Build brings the tree under root up to date with its scripts' sources,
// and returns the files that were (write) or would be (!write) changed,
// relative to root.
func Build(root string, write bool) ([]string, error) {
	srcs := map[string][]byte{}
	for _, sc := range Scripts {
		js, err := os.ReadFile(filepath.Join(root, Source(sc)))
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoSource
		}
		if err != nil {
			return nil, err
		}
		srcs[sc] = js
	}
	var changed []string
	note := func(rel string, want []byte) error {
		p := filepath.Join(root, rel)
		if got, err := os.ReadFile(p); err == nil && bytes.Equal(got, want) {
			return nil
		}
		changed = append(changed, rel)
		if !write {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, want, 0o644)
	}
	for _, tree := range Trees {
		assets := filepath.Join(tree, "assets")
		// The builds kept: this one first, then those before it, at most
		// Keep — a build being the set of its scripts' names, one line.
		var this []string
		for _, sc := range Scripts {
			this = append(this, Name(sc, srcs[sc]))
		}
		line := strings.Join(this, " ")
		builds := []string{line}
		if b, err := os.ReadFile(filepath.Join(root, assets, "builds.txt")); err == nil {
			for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
				if l = strings.TrimSpace(l); l != "" && l != line && len(builds) < Keep {
					builds = append(builds, l)
				}
			}
		}
		for _, sc := range Scripts {
			if err := note(filepath.Join(assets, Name(sc, srcs[sc])), srcs[sc]); err != nil {
				return nil, err
			}
		}
		if err := note(filepath.Join(assets, "builds.txt"), []byte(strings.Join(builds, "\n")+"\n")); err != nil {
			return nil, err
		}
		// Any other script in assets/ is past Keep: removed.
		keep := map[string]bool{"builds.txt": true}
		for _, b := range builds {
			for _, n := range strings.Fields(b) {
				keep[n] = true
			}
		}
		es, _ := os.ReadDir(filepath.Join(root, assets))
		for _, e := range es {
			if !keep[e.Name()] && strings.HasSuffix(e.Name(), ".js") {
				changed = append(changed, filepath.Join(assets, e.Name()))
				if write {
					if err := os.Remove(filepath.Join(root, assets, e.Name())); err != nil {
						return nil, err
					}
				}
			}
		}
		// The page's tags, stamped: exactly one for each script, in each copy.
		page := filepath.Join(tree, "index.html")
		html, err := os.ReadFile(filepath.Join(root, page))
		if err != nil {
			return nil, err
		}
		for _, sc := range Scripts {
			re := tagRE(sc)
			if n := len(re.FindAllIndex(html, -1)); n != 1 {
				return nil, fmt.Errorf("viewerbuild: %s has %d script tags for its assets/%s.*.js, want exactly 1", page, n, sc)
			}
			html = re.ReplaceAll(html, []byte(Tag(sc, srcs[sc])))
		}
		if err := note(page, html); err != nil {
			return nil, err
		}
	}
	return changed, nil
}
