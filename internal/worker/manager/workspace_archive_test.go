package manager

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestTarGzRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tarGzDir(dir, &buf); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	if err := untarGzDir(bytes.NewReader(buf.Bytes()), out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hi" {
		t.Fatalf("got %q", got)
	}
}

func TestUntarReplacesWhenCallerClears(t *testing.T) {
	// Mirrors UnpackWorkspace: wipe dest, then untar so leftovers cannot linger.
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep.txt"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := tarGzDir(src, &buf); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "stale.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dest); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := untarGzDir(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale file survived: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "keep.txt"))
	if err != nil || string(got) != "new" {
		t.Fatalf("keep.txt: %v %q", err, got)
	}
}

func TestUntarRejectsSymlinkAndTraversal(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	hdr := &tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o644}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gw.Close()
	if err := untarGzDir(bytes.NewReader(buf.Bytes()), t.TempDir()); err == nil {
		t.Fatal("expected symlink reject")
	}

	buf.Reset()
	gw = gzip.NewWriter(&buf)
	tw = tar.NewWriter(gw)
	hdr = &tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 3}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("bad")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gw.Close()
	if err := untarGzDir(bytes.NewReader(buf.Bytes()), t.TempDir()); err == nil {
		t.Fatal("expected traversal reject")
	}
}
