package api

import (
	"path/filepath"
	"testing"
)

func TestValidSnapshotID(t *testing.T) {
	ok, err := newSnapshotID()
	if err != nil {
		t.Fatal(err)
	}
	if !validSnapshotID(ok) {
		t.Fatalf("generated id rejected: %q", ok)
	}
	for _, bad := range []string{
		"",
		"abc",
		"../../../etc/passwd",
		ok + "/../x",
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // uppercase
		"0123456789abcdef0123456789abcdeg", // non-hex
	} {
		if validSnapshotID(bad) {
			t.Fatalf("accepted bad id %q", bad)
		}
	}
}

func TestSnapshotPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := snapshotPath(dir, "../../../tmp/evil")
	if err == nil {
		t.Fatal("expected reject")
	}
	id, err := newSnapshotID()
	if err != nil {
		t.Fatal(err)
	}
	path, err := snapshotPath(dir, id)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Clean(dir) {
		t.Fatalf("path escaped: %s", path)
	}
}
