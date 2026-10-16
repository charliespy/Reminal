// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 Harshal Gajjar

package client

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A folder is copied as a tar of its contents, taken when `reminal copy`
// runs, and sent like any file under the name "<folder>.tar" with the chunk's
// Archive set to "tar". A paste that knows Archive unpacks it into a folder of
// that name; an older paste saves the .tar, which is still the folder.

const archiveTar = "tar"

// copyTempPrefix names the temporary folder a snapshot is written to, so the
// background holder only ever removes a folder it was handed for that.
const copyTempPrefix = "reminal-copy-"

// prepareCopy returns what to send for path: path itself for a file, or a
// tar snapshot of a folder with archive set and a cleanup that removes it.
// skipped counts entries a snapshot leaves out (symlinks, devices, sockets).
func prepareCopy(path string) (src, archive string, skipped int, cleanup func(), err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", 0, nil, err
	}
	if !info.IsDir() {
		return path, "", 0, func() {}, validateCopyFile(path)
	}
	tmp, err := os.MkdirTemp("", copyTempPrefix)
	if err != nil {
		return "", "", 0, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	src = filepath.Join(tmp, folderName(path)+".tar")
	skipped, err = packFolder(path, src, downloadMaxBytes)
	if err != nil {
		cleanup()
		return "", "", 0, nil, err
	}
	return src, archiveTar, skipped, cleanup, nil
}

// folderName is the name a folder travels under: its own, or "folder" for a
// root such as "/" that has none.
func folderName(path string) string {
	name := filepath.Base(filepath.Clean(path))
	if name == "." || name == ".." || name == string(filepath.Separator) || name == "" || strings.HasSuffix(name, ":\\") {
		return "folder"
	}
	return name
}

// errFolderTooLarge is returned when a folder's snapshot would pass the copy
// size limit.
var errFolderTooLarge = fmt.Errorf("folder too large (max %s)", humanByteSize(downloadMaxBytes))

// countingWriter counts what passes through it and stops once limit is
// passed, so a large folder fails early instead of filling the disk.
type countingWriter struct {
	w     io.Writer
	n     int64
	limit int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	if c.n+int64(len(p)) > c.limit {
		return 0, errFolderTooLarge
	}
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// packFolder writes a tar of dir's contents to out. Only folders and regular
// files go in, with their permissions; anything else (symlinks, devices,
// sockets) is left out and counted, so a snapshot never points outside the
// folder it was taken of. Everything is walked and opened through an
// os.Root at dir, which refuses a path that leaves it at any level: a folder
// inside swapped for a link while the snapshot is taken can't lead outside.
// Its first entry, "./", carries dir's own permissions.
func packFolder(dir, out string, limit int64) (skipped int, err error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return 0, err
	}
	f, err := os.Create(out)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	tw := tar.NewWriter(&countingWriter{w: f, limit: limit})
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: int64(rootInfo.Mode().Perm())}); err != nil {
		return 0, err
	}
	err = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, werr error) error {
		if werr != nil {
			if errors.Is(werr, fs.ErrPermission) || isOutsideRoot(werr) {
				skipped++ // gone, or changed into a way out, since the walk saw it
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			return werr
		}
		if name == "." {
			return nil
		}
		switch {
		case d.IsDir():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: int64(info.Mode().Perm())})
		case d.Type().IsRegular():
			src, info, err := openRegular(root, name)
			if errors.Is(err, errNotRegular) || isOutsideRoot(err) {
				skipped++ // it changed into something else since the walk saw it
				return nil
			}
			if err != nil {
				return err
			}
			defer src.Close()
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: info.Size(), Mode: int64(info.Mode().Perm()), ModTime: info.ModTime()}); err != nil {
				return err
			}
			_, err = io.CopyN(tw, src, info.Size())
			return err
		default:
			skipped++
			return nil
		}
	})
	if err != nil {
		return skipped, err
	}
	return skipped, tw.Close()
}

var errNotRegular = errors.New("not a regular file")

// isOutsideRoot reports an os.Root refusing a path that would leave it.
func isOutsideRoot(err error) bool {
	return err != nil && strings.Contains(err.Error(), "path escapes from parent")
}

// openRegular opens name in root only if it is a regular file, and the file
// opened is the one at name: a file swapped for a link after the walk saw it
// is refused, not followed.
func openRegular(root *os.Root, name string) (*os.File, fs.FileInfo, error) {
	if lst, err := root.Lstat(name); err != nil {
		return nil, nil, err
	} else if !lst.Mode().IsRegular() {
		return nil, nil, errNotRegular
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err == nil {
		var lst fs.FileInfo
		lst, err = root.Lstat(name)
		if err == nil && (!info.Mode().IsRegular() || !lst.Mode().IsRegular() || !os.SameFile(info, lst)) {
			err = errNotRegular
		}
	}
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return f, info, nil
}

// A pasted folder is bounded in entries, in depth and in folders created,
// as well as in bytes: a deep enough path makes many folders out of few
// bytes.
const (
	maxArchiveEntries = 200000
	maxArchiveDepth   = 64
	maxArchiveDirs    = 20000
)

// pastedMode is the permission a pasted entry gets: the sender's, but never
// writable by group or others, and a folder always open to its owner, so
// the paste can be used and removed.
func pastedMode(mode int64, dir bool) os.FileMode {
	m := os.FileMode(mode).Perm() &^ 0o022
	if dir {
		m |= 0o700
	}
	return m
}

// unpackFolder writes the folder in raw (a tar from packFolder) and returns
// its absolute path, with notes on anything it renamed. dest is handled as
// for a file: an existing folder gets a new folder named after the copied one
// inside it; any other path is the folder to create.
//
// What it writes is bounded by the archive itself: every entry must stay
// inside that folder, only folders and regular files are written, a new
// folder is always created so nothing already on disk is written over, the
// files together can't hold more than the archive does, and any .git inside
// is renamed so a pasted folder is never a working repository.
func unpackFolder(dest, name string, raw []byte) (string, []string, error) {
	if dest == "" {
		dest = "."
	}
	base := strings.TrimSuffix(name, ".tar")
	if base == "" || base == "." || base == ".." {
		base = "pasted-folder"
	}
	target := dest
	if info, err := os.Stat(dest); err == nil && info.IsDir() {
		target = uniquePath(filepath.Join(dest, base))
	}
	if err := os.Mkdir(target, 0o700); err != nil {
		return "", nil, err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		abs = target
	}
	// Errors can name an entry the other machine chose: they leave as plain
	// text.
	fail := func(err error) (string, []string, error) {
		_ = os.RemoveAll(target)
		return "", nil, errors.New(plainText(err.Error(), 300))
	}
	made := map[string]bool{}
	// mkdirs makes p and its parents inside target, counting each folder.
	mkdirs := func(p string) error {
		rel, err := filepath.Rel(target, p)
		if err != nil || rel == "." {
			return err
		}
		cur := target
		for _, c := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, c)
			if !made[cur] {
				if len(made) >= maxArchiveDirs {
					return fmt.Errorf("the copied folder has more than %d folders", maxArchiveDirs)
				}
				made[cur] = true
			}
		}
		return os.MkdirAll(p, 0o700)
	}
	rootMode := os.FileMode(0o755)
	var dirs []string
	dirMode := map[string]os.FileMode{}
	renamed := map[string]string{}
	budget := int64(len(raw))
	tr := tar.NewReader(bytes.NewReader(raw))
	for entries := 0; ; entries++ {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail(fmt.Errorf("the copied folder is damaged: %w", err))
		}
		if entries >= maxArchiveEntries {
			return fail(fmt.Errorf("the copied folder has more than %d entries", maxArchiveEntries))
		}
		if isSparse(h) {
			return fail(errors.New("the copied folder is damaged: it has a sparse file"))
		}
		rel := filepath.FromSlash(strings.TrimSuffix(h.Name, "/"))
		if rel == "." && h.Typeflag == tar.TypeDir {
			rootMode = pastedMode(h.Mode, true)
			continue
		}
		if !filepath.IsLocal(rel) {
			return fail(fmt.Errorf("the copied folder has an entry outside it: %q", plainText(h.Name, 100)))
		}
		if len(strings.Split(rel, string(filepath.Separator))) > maxArchiveDepth {
			return fail(fmt.Errorf("the copied folder is nested more than %d deep", maxArchiveDepth))
		}
		rel, from, to := renameGit(rel)
		if from != "" {
			renamed[from] = to
		}
		p := filepath.Join(target, rel)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := mkdirs(p); err != nil {
				return fail(err)
			}
			if _, seen := dirMode[p]; !seen {
				dirs = append(dirs, p)
			}
			dirMode[p] = pastedMode(h.Mode, true)
		case tar.TypeReg:
			if h.Size < 0 || h.Size > budget {
				return fail(errors.New("the copied folder is damaged: its files are larger than it is"))
			}
			budget -= h.Size
			if err := mkdirs(filepath.Dir(p)); err != nil {
				return fail(err)
			}
			f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return fail(err)
			}
			_, err = io.CopyN(f, tr, h.Size)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err == nil {
				err = os.Chmod(p, pastedMode(h.Mode, false))
			}
			if err != nil {
				return fail(err)
			}
		default:
			// Links and special files are never written.
		}
	}
	gitNotes, err := disarmRepos(target)
	if err != nil {
		return fail(err)
	}
	// Folders get their permissions last, deepest first, so a read-only one
	// was still writable while its contents went in.
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Chmod(dirs[i], dirMode[dirs[i]])
	}
	_ = os.Chmod(target, rootMode)
	var notes []string
	for _, from := range sortedKeys(renamed) {
		notes = append(notes, fmt.Sprintf("Renamed %s to %s so the pasted folder isn't a working Git repository. Rename it back if you trust where it came from.",
			plainText(from, 200), plainText(renamed[from], 200)))
	}
	return abs, append(notes, gitNotes...), nil
}

// disarmRepos finds every folder under root that Git would take for a
// repository whatever its name (a HEAD beside objects, refs or commondir, as
// in a bare repository or a renamed .git) and renames its HEAD to
// HEAD.pasted, so no Git command treats it as one and runs what its config
// or hooks say.
func disarmRepos(root string) ([]string, error) {
	var notes []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return err
		}
		var head string
		repoish := false
		for _, e := range entries {
			n := strings.ToLower(withoutFormatChars(e.Name()))
			switch {
			case n == "head" && !e.IsDir():
				head = e.Name()
			case (n == "objects" || n == "refs") && e.IsDir(), n == "commondir" && !e.IsDir():
				repoish = true
			}
		}
		if head == "" || !repoish {
			return nil
		}
		dst := uniquePath(filepath.Join(p, "HEAD.pasted"))
		if err := os.Rename(filepath.Join(p, head), dst); err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if rel == "." {
			rel = filepath.Base(root)
		}
		notes = append(notes, fmt.Sprintf("Renamed %s to %s so %s isn't a working Git repository. Rename it back if you trust where it came from.",
			plainText(filepath.Join(rel, head), 200), plainText(filepath.Join(rel, filepath.Base(dst)), 200), plainText(rel, 200)))
		return nil
	})
	return notes, err
}

// isSparse reports a sparse file entry: its logical size isn't in the
// archive, so it is refused rather than expanded.
func isSparse(h *tar.Header) bool {
	if h.Typeflag == tar.TypeGNUSparse {
		return true
	}
	for k := range h.PAXRecords {
		if strings.HasPrefix(k, "GNU.sparse.") {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// removeCopyTemp removes the temporary folder a snapshot at src was written
// to, and nothing else: only a folder prepareCopy made qualifies.
func removeCopyTemp(src string) {
	dir := filepath.Dir(src)
	if strings.HasPrefix(filepath.Base(dir), copyTempPrefix) && filepath.Dir(dir) == filepath.Clean(os.TempDir()) {
		_ = os.RemoveAll(dir)
	}
}
