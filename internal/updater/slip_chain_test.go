package updater

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// A bundle whose symlinks each stay inside it when read alone, but lead out
// of it when followed one through another, writes nothing outside it.
func TestApplyBundleChainedSymlinksStayInside(t *testing.T) {
	parent := t.TempDir()
	appRoot := filepath.Join(parent, "reminal.app")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(h *tar.Header, body string) {
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			if _, err := tw.Write([]byte(body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(&tar.Header{Name: "reminal.app/", Typeflag: tar.TypeDir, Mode: 0o755}, "")
	write(&tar.Header{Name: "reminal.app/l", Typeflag: tar.TypeSymlink, Linkname: ".."}, "")
	write(&tar.Header{Name: "reminal.app/l/m", Typeflag: tar.TypeSymlink, Linkname: ".."}, "")
	write(&tar.Header{Name: "reminal.app/l/m/outside", Typeflag: tar.TypeReg, Mode: 0o644}, "x")
	write(&tar.Header{Name: "reminal.app/Contents/A", Typeflag: tar.TypeReg, Mode: 0o644}, "real")
	write(&tar.Header{Name: "reminal.app/Contents/Current", Typeflag: tar.TypeSymlink, Linkname: "A"}, "")
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = applyBundle(tar.NewReader(&buf), appRoot)
	if _, err := os.Lstat(filepath.Join(parent, "outside")); err == nil {
		t.Fatal("a file was written beside the bundle, outside it")
	}
}
