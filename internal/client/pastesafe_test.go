// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// copyFileThrough sends one file from a source to a paste, as copyFolderThrough
// does for a folder.
func copyFileThrough(t *testing.T, file, dest string) string {
	t.Helper()
	const code = "ABCDEFGHJK"
	srcConn, pasteConn := newMemPair()
	srcErr := make(chan error, 1)
	go func() { srcErr <- runSource(srcConn, code, file, "") }()
	done := make(chan string, 1)
	go func() {
		p, err := runPaste(pasteConn, code, dest)
		if err != nil {
			t.Errorf("paste: %v", err)
		}
		done <- p
	}()
	if err := <-srcErr; err != nil {
		t.Fatalf("source: %v", err)
	}
	close(srcConn.out)
	return <-done
}

// A folder whose name starts with a dot doesn't land as that hidden folder:
// .git pasted into a project would be its repository, with the sender's
// config, and .ssh pasted into a home folder its keys.
func TestPasteDotNamedFolderIsPrefixed(t *testing.T) {
	for _, name := range []string{".git", ".ssh", ".vscode"} {
		dir := filepath.Join(t.TempDir(), name)
		writeTree(t, dir, map[string]string{"config": "[core]\n\tfsmonitor = touch /tmp/ran\n"})
		dest := t.TempDir()
		got := copyFolderThrough(t, dir, dest)
		if filepath.Base(got) != pastedPrefix+name {
			t.Fatalf("%s pasted as %q, want %q", name, filepath.Base(got), pastedPrefix+name)
		}
		if _, err := os.Lstat(filepath.Join(dest, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s landed as it is: %v", name, err)
		}
	}
}

// The same for a single file, such as .bashrc pasted into a home folder.
func TestPasteDotNamedFileIsPrefixed(t *testing.T) {
	src := filepath.Join(t.TempDir(), ".bashrc")
	if err := os.WriteFile(src, []byte("echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	got := copyFileThrough(t, src, dest)
	if filepath.Base(got) != pastedPrefix+".bashrc" {
		t.Fatalf("pasted as %q", filepath.Base(got))
	}
	if _, err := os.Lstat(filepath.Join(dest, ".bashrc")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf(".bashrc landed as it is: %v", err)
	}
}

// A .git inside a pasted folder, in any case, is renamed, so the folder is
// never a working repository whose config and hooks run commands.
func TestUnpackFolderRenamesGit(t *testing.T) {
	raw := craftedTar(t,
		tar.Header{Typeflag: tar.TypeDir, Name: ".git/", Mode: 0o755},
		tar.Header{Typeflag: tar.TypeReg, Name: ".git/config", Mode: 0o644},
		tar.Header{Typeflag: tar.TypeReg, Name: ".git/hooks/post-checkout", Mode: 0o755},
		tar.Header{Typeflag: tar.TypeReg, Name: "sub/.GIT/config", Mode: 0o644},
		// A gitfile, in a folder of its own: beside sub/.GIT it would be the
		// same name on a case-insensitive file system.
		tar.Header{Typeflag: tar.TypeReg, Name: "sub2/.git", Mode: 0o644},
		// Windows reads ".git." as .git.
		tar.Header{Typeflag: tar.TypeReg, Name: "sub3/.git./config", Mode: 0o644},
		tar.Header{Typeflag: tar.TypeReg, Name: "src/main.go", Mode: 0o644},
	)
	dest := t.TempDir()
	got, notes, err := unpackFolder(dest, "proj.tar", raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".", "sub", "sub2", "sub3"} {
		entries, _ := os.ReadDir(filepath.Join(got, d))
		for _, e := range entries {
			if isGitName(e.Name()) {
				t.Fatalf("%s/%s was written", d, e.Name())
			}
		}
	}
	if _, err := os.Stat(filepath.Join(got, "pasted-.git", "config")); err != nil {
		t.Fatalf("renamed .git missing: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(got, "src", "main.go")); string(b) != "content" {
		t.Fatal("the rest of the folder wasn't written")
	}
	if all := strings.Join(notes, "\n"); len(notes) != 4 || !strings.Contains(all, "Renamed .git to pasted-.git") {
		t.Fatalf("notes don't say what was renamed: %q", notes)
	}
}

// sparseArchive is a tar whose one entry is a GNU sparse 1.0 file: a few bytes in
// the archive that read back as size bytes.
func sparseArchive(t *testing.T, size int64) []byte {
	t.Helper()
	block := func(b []byte) []byte {
		return append(b, make([]byte, (512-len(b)%512)%512)...)
	}
	header := func(name string, flag byte, size int64) []byte {
		h := make([]byte, 512)
		copy(h[0:], name)
		copy(h[100:], "0000644\x00")
		copy(h[108:], "0000000\x00")
		copy(h[116:], "0000000\x00")
		copy(h[124:], fmt.Sprintf("%011o\x00", size))
		copy(h[136:], "00000000000\x00")
		h[156] = flag
		copy(h[257:], "ustar\x0000")
		copy(h[148:], "        ")
		sum := 0
		for _, c := range h {
			sum += int(c)
		}
		copy(h[148:], fmt.Sprintf("%06o\x00 ", sum))
		return h
	}
	rec := func(k, v string) string {
		r := " " + k + "=" + v + "\n"
		n := len(r)
		for len(fmt.Sprint(n))+len(r) != n {
			n = len(fmt.Sprint(n)) + len(r)
		}
		return fmt.Sprint(n) + r
	}
	pax := rec("GNU.sparse.major", "1") + rec("GNU.sparse.minor", "0") +
		rec("GNU.sparse.name", "big") + rec("GNU.sparse.realsize", fmt.Sprint(size))
	sparseMap := block([]byte(fmt.Sprintf("1\n%d\n1\n", size-1)))
	data := block([]byte("x"))
	var out []byte
	out = append(out, header("PaxHeaders/big", 'x', int64(len(pax)))...)
	out = append(out, block([]byte(pax))...)
	out = append(out, header("big", '0', int64(len(sparseMap)+1))...)
	out = append(out, sparseMap...)
	out = append(out, data...)
	return append(out, make([]byte, 1024)...)
}

// A pasted folder can't write more than the archive holds: a sparse entry of a
// few bytes that reads back as 128 MiB is refused, and nothing is left.
func TestUnpackFolderRefusesSparseEntries(t *testing.T) {
	raw := sparseArchive(t, 1<<27)
	// A reader that expands it writes the full size.
	h, err := tar.NewReader(bytes.NewReader(raw)).Next()
	if err != nil || h.Size != 1<<27 {
		t.Fatalf("test archive isn't what it should be: %v %v", h, err)
	}
	dest := t.TempDir()
	if _, _, err := unpackFolder(dest, "x.tar", raw); err == nil {
		t.Fatal("a sparse entry was expanded")
	}
	if _, err := os.Stat(filepath.Join(dest, "x")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("something was left behind: %v", err)
	}
}

// An entry may not claim more bytes than the archive has left, sparse or not.
func TestUnpackFolderBudget(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "a", Mode: 0o644, Size: 4})
	_, _ = tw.Write([]byte("abcd"))
	_ = tw.Close()
	raw := buf.Bytes()
	if _, _, err := unpackFolder(t.TempDir(), "ok.tar", raw); err != nil {
		t.Fatalf("a normal archive was refused: %v", err)
	}
}

// Permissions survive the trip: owner-only stays owner-only.
func TestCopyFolderKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps only a read-only bit")
	}
	dir := filepath.Join(t.TempDir(), "keys")
	writeTree(t, dir, map[string]string{"id_ed25519": "secret", "inner/token": "t", "run.sh": "#!/bin/sh\n"})
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Chmod(filepath.Join(dir, "id_ed25519"), 0o600))
	must(os.Chmod(filepath.Join(dir, "inner", "token"), 0o600))
	must(os.Chmod(filepath.Join(dir, "inner"), 0o700))
	must(os.Chmod(filepath.Join(dir, "run.sh"), 0o750))
	must(os.Chmod(dir, 0o700))
	got := copyFolderThrough(t, dir, t.TempDir())
	for name, want := range map[string]os.FileMode{".": 0o700, "id_ed25519": 0o600, "inner": 0o700, "inner/token": 0o600, "run.sh": 0o750} {
		fi, err := os.Stat(filepath.Join(got, filepath.FromSlash(name)))
		if err != nil || fi.Mode().Perm() != want {
			t.Fatalf("%s: %v, want %v (%v)", name, fi.Mode().Perm(), want, err)
		}
	}
}

// A file swapped for a link after the walk saw it is refused, not followed
// out of the folder.
func TestOpenRegularRefusesALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(secret, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "swapped")); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o644)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if f, _, err := openRegular(root, "swapped"); err == nil {
		f.Close()
		t.Fatal("openRegular followed a link")
	}
	f, _, err := openRegular(root, "plain")
	if err != nil {
		t.Fatalf("a regular file was refused: %v", err)
	}
	f.Close()
}

// A folder inside swapped for a link to one outside, while the snapshot is
// being taken, never puts the outside folder's files in it.
func TestPackFolderParentSwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	secretDir := filepath.Join(base, "secret")
	_ = os.Mkdir(secretDir, 0o700)
	_ = os.WriteFile(filepath.Join(secretDir, "id_ed25519"), []byte("OUTSIDE-ONLY"), 0o600)
	shared := filepath.Join(base, "shared")
	_ = os.Mkdir(shared, 0o755)
	real := filepath.Join(shared, "a")
	_ = os.Mkdir(real, 0o755)
	_ = os.WriteFile(filepath.Join(real, "id_ed25519"), []byte("inside"), 0o644)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		link := filepath.Join(shared, "lnk")
		_ = os.Symlink(secretDir, link)
		tmp := filepath.Join(shared, "a_tmp")
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Rename(real, tmp)
			_ = os.Rename(link, real)
			_ = os.Rename(real, link)
			_ = os.Rename(tmp, real)
		}
	}()
	defer func() { close(stop); <-done }()
	out := filepath.Join(base, "o.tar")
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if _, err := packFolder(shared, out, 1<<20); err != nil {
			continue
		}
		if b, _ := os.ReadFile(out); bytes.Contains(b, []byte("OUTSIDE-ONLY")) {
			t.Fatal("a file from outside the folder went into its snapshot")
		}
	}
}

// Names and messages from another machine reach the terminal as plain text.
func TestPlainTextAndReceivedNames(t *testing.T) {
	if got := plainText("a\x1b]0;evil\x07b\u009b31m‮c", 100); got != "a]0;evilb31mc" {
		t.Fatalf("plainText = %q", got)
	}
	if got := plainText(strings.Repeat("x", 50), 10); got != strings.Repeat("x", 10)+"…" {
		t.Fatalf("not capped: %q", got)
	}
	raw := craftedTar(t, tar.Header{Typeflag: tar.TypeReg, Name: "../\x1b]0;title\x07", Mode: 0o644})
	_, _, err := unpackFolder(t.TempDir(), "x.tar", raw)
	if err == nil || strings.ContainsAny(err.Error(), "\x1b\x07") {
		t.Fatalf("error carries control characters: %q", err)
	}
	names := map[string]string{"notes.txt": "notes.txt", ".bashrc": "pasted-.bashrc", "..": "fb", "": "fb", "a\x1b[2Jb": "a[2Jb"}
	if runtime.GOOS == "windows" {
		// CON alone is a device; Windows 11 takes CON.tar as an ordinary name.
		names["CON"] = "fb"
		names["a:b"] = "b" // a drive letter, dropped; never a:b's stream
	}
	for in, want := range names {
		if got, _ := receivedName(in, "fb"); got != want {
			t.Fatalf("receivedName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A forward whose result never reaches `reminal expose` (it stopped waiting)
// ends, whether it had credentials or a reason to send.
func TestTunnelEndsWhenExposeIsGone(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gone := ln.Addr().String()
	ln.Close() // the parent's listener, closed after its timeout

	tn := &Tunnel{sessionID: "ABCDEFGH", port: 3000, handshakeAddr: gone}
	if !tn.writeHandshakeError() {
		t.Fatal("give-up didn't end the forward when the reason couldn't be delivered")
	}

	ended := false
	tn = &Tunnel{sessionID: "ABCDEFGH", port: 3000, handshakeAddr: gone, webURL: "http://127.0.0.1:1"}
	tn.abandon = func() { ended = true }
	tn.writeHandshake()
	if !ended {
		t.Fatal("a forward whose credentials couldn't be delivered stayed up")
	}

	// Delivered credentials keep it up.
	r, w, _ := os.Pipe()
	defer r.Close()
	ended = false
	tn = &Tunnel{sessionID: "ABCDEFGH", port: 3000, handshakeFD: int(w.Fd()), webURL: "http://127.0.0.1:1"}
	tn.abandon = func() { ended = true }
	tn.writeHandshake()
	if ended {
		t.Fatal("a forward that delivered its credentials was ended")
	}
}

// tarOf builds a tar from headers, with data for the regular files.
func tarOf(t *testing.T, ents []tar.Header, data map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range ents {
		h := h
		if d, ok := data[h.Name]; ok {
			h.Size = int64(len(d))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if d, ok := data[h.Name]; ok {
			_, _ = tw.Write([]byte(d))
		}
	}
	_ = tw.Close()
	return buf.Bytes()
}

// A folder Git would take for a repository is never a working one after a
// paste, whatever it is called: a bare repository, one whose config points
// its work tree elsewhere, and a renamed .git. git status in it must not run
// what its config says.
func TestUnpackFolderDisarmsRepos(t *testing.T) {
	git, gitErr := exec.LookPath("git")
	for _, c := range []struct{ name, dir, extra string }{
		{"bare", "sub", "\tbare = true\n"},
		{"work tree elsewhere", "meta", "\tbare = false\n\tworktree = ..\n"},
		{"renamed .git", ".git", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			dest := t.TempDir()
			marker := filepath.Join(dest, "ran")
			cfg := "[core]\n\trepositoryformatversion = 0\n" + c.extra + "\tfsmonitor = \"touch " + filepath.ToSlash(marker) + "; false\"\n"
			d := c.dir
			raw := tarOf(t, []tar.Header{
				{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755},
				{Typeflag: tar.TypeReg, Name: "README", Mode: 0o644},
				{Typeflag: tar.TypeDir, Name: d + "/", Mode: 0o755},
				{Typeflag: tar.TypeReg, Name: d + "/HEAD", Mode: 0o644},
				{Typeflag: tar.TypeReg, Name: d + "/config", Mode: 0o644},
				{Typeflag: tar.TypeDir, Name: d + "/objects/", Mode: 0o755},
				{Typeflag: tar.TypeDir, Name: d + "/refs/", Mode: 0o755},
			}, map[string]string{"README": "hi", d + "/HEAD": "ref: refs/heads/main\n", d + "/config": cfg})
			got, notes, err := unpackFolder(dest, "proj.tar", raw)
			if err != nil {
				t.Fatal(err)
			}
			repo := filepath.Join(got, d)
			if d == ".git" {
				repo = filepath.Join(got, pastedPrefix+".git")
			}
			if gitErr == nil {
				for _, dir := range []string{repo, got} {
					cmd := exec.Command(git, "status")
					cmd.Dir = dir
					cmd.Env = append(os.Environ(), "GIT_CEILING_DIRECTORIES="+filepath.Dir(got))
					_, _ = cmd.CombinedOutput()
				}
				if _, err := os.Stat(marker); err == nil {
					t.Fatal("git status in the pasted folder ran the command in its config")
				}
			} else {
				t.Logf("git not found: %v", gitErr)
			}
			if _, err := os.Stat(filepath.Join(repo, "HEAD")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s still has its HEAD: %v", repo, err)
			}
			if !strings.Contains(strings.Join(notes, "\n"), "isn't a working Git repository") {
				t.Fatalf("no note says what was done: %q", notes)
			}
		})
	}
}

// Names that differ from .git only by invisible formatting characters, which
// some file systems ignore, count as .git.
func TestIsGitNameIgnoresFormatChars(t *testing.T) {
	for _, n := range []string{".git", ".GIT", ".git.", ".g\u200bit", "\ufeff.git", ".gi\u200dt"} {
		if !isGitName(n) {
			t.Fatalf("%q isn't taken for .git", n)
		}
	}
	if isGitName(".github") || isGitName("git") {
		t.Fatal("a name that isn't .git was taken for it")
	}
}

// Errors from unpacking can name an entry the other machine chose; they
// reach the terminal as plain text.
func TestUnpackErrorsArePlain(t *testing.T) {
	name := "a\x1b]0;title\x07\x1b[2J"
	raw := tarOf(t, []tar.Header{
		{Typeflag: tar.TypeReg, Name: name, Mode: 0o644},
		{Typeflag: tar.TypeReg, Name: name, Mode: 0o644},
	}, nil)
	_, _, err := unpackFolder(t.TempDir(), "p.tar", raw)
	if err == nil {
		t.Fatal("a repeated entry was accepted")
	}
	if strings.ContainsAny(err.Error(), "\x1b\x07") {
		t.Fatalf("the error carries control characters: %q", err)
	}
}

// Depth and the number of folders are bounded, not only bytes.
func TestUnpackFolderLimits(t *testing.T) {
	deep := strings.Repeat("d/", maxArchiveDepth+1) + "f"
	if _, _, err := unpackFolder(t.TempDir(), "deep.tar", tarOf(t, []tar.Header{{Typeflag: tar.TypeReg, Name: deep, Mode: 0o644}}, nil)); err == nil {
		t.Fatal("a path deeper than the limit was unpacked")
	}
	ents := make([]tar.Header, 0, maxArchiveDirs+1)
	for i := 0; i <= maxArchiveDirs; i++ {
		ents = append(ents, tar.Header{Typeflag: tar.TypeDir, Name: fmt.Sprintf("d%d/", i), Mode: 0o755})
	}
	dest := t.TempDir()
	if _, _, err := unpackFolder(dest, "many.tar", tarOf(t, ents, nil)); err == nil {
		t.Fatal("more folders than the limit were created")
	}
	if _, err := os.Stat(filepath.Join(dest, "many")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a refused folder was left behind: %v", err)
	}
}

// The sender's permissions, but never writable by others, never set-id, and
// a folder always open to its owner.
func TestPastedModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps only a read-only bit")
	}
	raw := tarOf(t, []tar.Header{
		{Typeflag: tar.TypeDir, Name: "./", Mode: 0o7777},
		{Typeflag: tar.TypeDir, Name: "d/", Mode: 0},
		{Typeflag: tar.TypeReg, Name: "d/f", Mode: 0o6777},
	}, map[string]string{"d/f": "x"})
	got, _, err := unpackFolder(t.TempDir(), "p.tar", raw)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]os.FileMode{".": 0o755, "d": 0o700, "d/f": 0o755} {
		fi, err := os.Stat(filepath.Join(got, filepath.FromSlash(name)))
		if err != nil || fi.Mode()&(os.ModePerm|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != want {
			t.Fatalf("%s: %v, want %v (%v)", name, fi.Mode(), want, err)
		}
	}
}
