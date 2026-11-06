package manager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDiskBytes(t *testing.T) {
	n, err := parseDiskBytes("10Gi")
	if err != nil || n != 10*(1<<30) {
		t.Fatalf("10Gi: %d %v", n, err)
	}
	n, err = parseDiskBytes("100Mi")
	if err != nil || n != 100*(1<<20) {
		t.Fatalf("100Mi: %d %v", n, err)
	}
}

func TestCheckWorkspaceDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big"), make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkspaceDisk(dir, "1Ki"); err == nil {
		t.Fatal("expected over-limit error")
	}
	if err := checkWorkspaceDisk(dir, "10Mi"); err != nil {
		t.Fatal(err)
	}
	if err := checkWorkspaceDisk(dir, ""); err != nil {
		t.Fatal(err)
	}
}
