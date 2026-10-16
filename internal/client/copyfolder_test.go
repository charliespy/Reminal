// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"archive/tar"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// copyFolderThrough sends dir from a source to a paste over an in-memory
// connection, the whole handshake included, and returns what the paste wrote.
func copyFolderThrough(t *testing.T, dir, dest string) string {
	t.Helper()
	src, archive, _, cleanup, err := prepareCopy(dir)
	if err != nil {
		t.Fatalf("prepareCopy: %v", err)
	}
	defer cleanup()
	if archive != archiveTar || filepath.Base(src) != filepath.Base(dir)+".tar" {
		t.Fatalf("prepareCopy(folder) = %q, %q; want a .tar named after it", src, archive)
	}
	const code = "ABCDEFGHJK"
	srcConn, pasteConn := newMemPair()
	srcErr := make(chan error, 1)
	go func() { srcErr <- runSource(srcConn, code, src, archive) }()
	type res struct {
		path string
		err  error
	}
	pasted := make(chan res, 1)
	go func() {
		p, e := runPaste(pasteConn, code, dest)
		pasted <- res{p, e}
	}()
	if err := <-srcErr; err != nil {
		t.Fatalf("source: %v", err)
	}
	close(srcConn.out)
	r := <-pasted
	if r.err != nil {
		t.Fatalf("paste: %v", r.err)
	}
	return r.path
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCopyFolderRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "project")
	files := map[string]string{
		"README.md":         "hello\n",
		"src/main.go":       "package main\n",
		"src/deep/a/b.txt":  strings.Repeat("x", 3*downloadChunkBytes+7), // spans chunks
		"empty.txt":         "",
		"with space/n m.md": "spaces\n",
	}
	writeTree(t, dir, files)
	if err := os.MkdirAll(filepath.Join(dir, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(filepath.Join(dir, "src/main.go"), 0o755); err != nil {
			t.Fatal(err)
		}
		// A link is left out of the snapshot, never followed.
		if err := os.Symlink("/etc/passwd", filepath.Join(dir, "link")); err != nil {
			t.Fatal(err)
		}
	}

	dest := t.TempDir()
	got := copyFolderThrough(t, dir, dest)
	if filepath.Base(got) != "project" || filepath.Dir(got) != dest {
		t.Fatalf("pasted to %q, want %q", got, filepath.Join(dest, "project"))
	}
	for name, body := range files {
		b, err := os.ReadFile(filepath.Join(got, filepath.FromSlash(name)))
		if err != nil || string(b) != body {
			t.Fatalf("%s: got %d bytes, err %v", name, len(b), err)
		}
	}
	if fi, err := os.Stat(filepath.Join(got, "empty-dir")); err != nil || !fi.IsDir() {
		t.Fatalf("empty folder not recreated: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(got, "link")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("link was copied: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(got, "src/main.go")); fi.Mode()&0o111 == 0 {
			t.Fatalf("executable bit lost: %v", fi.Mode())
		}
	}

	// Pasting the same folder again makes a second one beside it.
	again := copyFolderThrough(t, dir, dest)
	if again == got || filepath.Dir(again) != dest {
		t.Fatalf("second paste went to %q (first %q)", again, got)
	}
	// A destination that doesn't exist yet is the folder to create.
	named := filepath.Join(dest, "renamed")
	if p := copyFolderThrough(t, dir, named); p != named {
		t.Fatalf("paste to new path %q went to %q", named, p)
	}
}

func TestCopyFolderSnapshotIsRemoved(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "f")
	writeTree(t, dir, map[string]string{"a": "1"})
	src, _, _, cleanup, err := prepareCopy(dir)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(filepath.Dir(src)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot folder left behind: %v", err)
	}
	// removeCopyTemp touches only a snapshot folder.
	keep := filepath.Join(t.TempDir(), "keep.tar")
	if err := os.WriteFile(keep, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	removeCopyTemp(keep)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("removeCopyTemp removed something it didn't make: %v", err)
	}
}

func TestCopyFolderTooLarge(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "big")
	writeTree(t, dir, map[string]string{"a": strings.Repeat("y", 4096)})
	out := filepath.Join(t.TempDir(), "big.tar")
	if _, err := packFolder(dir, out, 2048); !errors.Is(err, errFolderTooLarge) {
		t.Fatalf("packFolder over the limit = %v, want errFolderTooLarge", err)
	}
}

// craftedTar builds a tar entry by entry, as any sender could, whether or
// not packFolder would have written it that way.
func craftedTar(t *testing.T, entries ...tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range entries {
		h := h
		body := ""
		if h.Typeflag == tar.TypeReg {
			body = "content"
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUnpackFolderStaysInside(t *testing.T) {
	cases := map[string][]tar.Header{
		"parent":        {{Typeflag: tar.TypeReg, Name: "../escaped", Mode: 0o644}},
		"nested parent": {{Typeflag: tar.TypeReg, Name: "a/../../escaped", Mode: 0o644}},
		"absolute":      {{Typeflag: tar.TypeReg, Name: "/tmp/escaped", Mode: 0o644}},
		"dir parent":    {{Typeflag: tar.TypeDir, Name: "../escaped-dir/", Mode: 0o755}},
	}
	if runtime.GOOS == "windows" {
		cases["drive"] = []tar.Header{{Typeflag: tar.TypeReg, Name: "C:/escaped", Mode: 0o644}}
		cases["backslash"] = []tar.Header{{Typeflag: tar.TypeReg, Name: `..\escaped`, Mode: 0o644}}
		cases["reserved name"] = []tar.Header{{Typeflag: tar.TypeReg, Name: "CON", Mode: 0o644}}
		cases["stream"] = []tar.Header{{Typeflag: tar.TypeReg, Name: "a:b", Mode: 0o644}}
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "in")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			_, _, err := unpackFolder(dest, "x.tar", craftedTar(t, entries...))
			if err == nil {
				t.Fatal("unpackFolder accepted an entry outside the folder")
			}
			for _, p := range []string{filepath.Join(parent, "escaped"), filepath.Join(dest, "escaped"), filepath.Join(parent, "escaped-dir")} {
				if _, err := os.Lstat(p); err == nil {
					t.Fatalf("%s was written", p)
				}
			}
			if _, err := os.Stat(filepath.Join(dest, "x")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a refused folder was left half-written: %v", err)
			}
		})
	}
}

func TestUnpackFolderWritesNoLinks(t *testing.T) {
	dest := t.TempDir()
	raw := craftedTar(t,
		tar.Header{Typeflag: tar.TypeSymlink, Name: "ln", Linkname: "/etc", Mode: 0o777},
		tar.Header{Typeflag: tar.TypeLink, Name: "hard", Linkname: "/etc/passwd", Mode: 0o644},
		tar.Header{Typeflag: tar.TypeReg, Name: "ok.txt", Mode: 0o644},
	)
	got, _, err := unpackFolder(dest, "x.tar", raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"ln", "hard"} {
		if _, err := os.Lstat(filepath.Join(got, n)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s was written: %v", n, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(got, "ok.txt")); string(b) != "content" {
		t.Fatalf("regular file not written")
	}
}

func TestUnpackFolderNeverWritesOverAFile(t *testing.T) {
	dest := t.TempDir()
	existing := filepath.Join(dest, "taken")
	if err := os.WriteFile(existing, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Asked to create the folder at a path that is a file: refused.
	if _, _, err := unpackFolder(existing, "x.tar", craftedTar(t, tar.Header{Typeflag: tar.TypeReg, Name: "a", Mode: 0o644})); err == nil {
		t.Fatal("unpacked over an existing file")
	}
	if b, _ := os.ReadFile(existing); string(b) != "mine" {
		t.Fatal("existing file changed")
	}
	// The same entry twice can't overwrite the first.
	if _, _, err := unpackFolder(dest, "dup.tar", craftedTar(t,
		tar.Header{Typeflag: tar.TypeReg, Name: "a", Mode: 0o644},
		tar.Header{Typeflag: tar.TypeReg, Name: "a", Mode: 0o644},
	)); err == nil {
		t.Fatal("a repeated entry was accepted")
	}
}

// An older paste knows nothing of Archive: it saves the folder as its .tar.
func TestCopyFolderToOlderPaste(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "proj")
	writeTree(t, dir, map[string]string{"a.txt": "a"})
	src, _, _, cleanup, err := prepareCopy(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	const code = "ABCDEFGHJK"
	srcConn, pasteConn := newMemPair()
	srcErr := make(chan error, 1)
	// The same bytes, sent with no Archive, are what an older paste sees.
	go func() { srcErr <- runSource(srcConn, code, src, "") }()
	dest := t.TempDir()
	done := make(chan string, 1)
	go func() {
		p, _ := runPaste(pasteConn, code, dest)
		done <- p
	}()
	if err := <-srcErr; err != nil {
		t.Fatal(err)
	}
	close(srcConn.out)
	p := <-done
	if filepath.Base(p) != "proj.tar" {
		t.Fatalf("older paste saved %q, want proj.tar", p)
	}
	raw, _ := os.ReadFile(p)
	tr := tar.NewReader(bytes.NewReader(raw))
	h, err := tr.Next()
	if err == nil && h.Name == "./" {
		h, err = tr.Next() // the folder's own permissions come first
	}
	if err != nil || h.Name != "a.txt" {
		t.Fatalf("saved .tar isn't the folder: %v %v", h, err)
	}
}
