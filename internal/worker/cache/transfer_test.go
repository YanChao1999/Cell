package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDirIsWarmAndPackUnpack(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(root)
	if err := layout.Ensure(); err != nil {
		t.Fatal(err)
	}
	key := "go-mod"
	src := filepath.Join(root, key)
	if DirIsWarm(src) {
		t.Fatal("empty dir should be cold")
	}
	if err := os.WriteFile(filepath.Join(src, "pkg.mod"), []byte("module example"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !DirIsWarm(src) {
		t.Fatal("expected warm")
	}
	warm := layout.WarmKeys()
	if len(warm) != 1 || warm[0] != key {
		t.Fatalf("WarmKeys=%v", warm)
	}

	var buf bytes.Buffer
	if err := layout.PackTarGzip(&buf, key, TransferOptions{}); err != nil {
		t.Fatal(err)
	}

	destRoot := t.TempDir()
	dest := NewLayout(destRoot)
	_ = dest.Ensure()
	if err := dest.UnpackTarGzip(bytes.NewReader(buf.Bytes()), key, TransferOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destRoot, key, "pkg.mod"))
	if err != nil || string(got) != "module example" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestSeedFromLayoutWarmOnly(t *testing.T) {
	root := t.TempDir()
	layout := NewLayout(root)
	_ = layout.Ensure()
	idx := NewIndex()
	idx.SeedFromLayout(layout)
	if len(idx.Keys()) != 0 {
		t.Fatalf("empty layout should seed no keys, got %v", idx.Keys())
	}
	_ = os.WriteFile(filepath.Join(root, "pip-cache", "a"), []byte("x"), 0o644)
	idx.SeedFromLayout(layout)
	if !idx.Has("pip-cache") {
		t.Fatal("expected pip-cache")
	}
}
