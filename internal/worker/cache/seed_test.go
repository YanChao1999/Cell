package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedCopyWarmAndNoHardlinkPoison(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	dest := filepath.Join(root, "cell")
	if err := os.MkdirAll(filepath.Join(shared, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("module-bytes")
	if err := os.WriteFile(filepath.Join(shared, "pkg", "x.mod"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedCopy(shared, dest, SeedOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "pkg", "x.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("seed content: %q", got)
	}
	// Mutating the Cell copy must not change shared.
	if err := os.WriteFile(filepath.Join(dest, "pkg", "x.mod"), []byte("pwned"), 0o644); err != nil {
		t.Fatal(err)
	}
	sharedGot, err := os.ReadFile(filepath.Join(shared, "pkg", "x.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sharedGot) != string(body) {
		t.Fatalf("shared poisoned via seed: %q", sharedGot)
	}
}

func TestSeedCopyRefusesSymlink(t *testing.T) {
	root := t.TempDir()
	shared := filepath.Join(root, "shared")
	dest := filepath.Join(root, "cell")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(shared, "x")); err != nil {
		t.Fatal(err)
	}
	if err := SeedCopy(shared, dest, SeedOptions{}); err == nil {
		t.Fatal("expected symlink refusal")
	}
}
