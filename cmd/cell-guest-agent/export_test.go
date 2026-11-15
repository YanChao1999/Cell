package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/YanChao1999/Cell/internal/worker/runtime/microvm/guestproto"
)

func TestExportWorkspace(t *testing.T) {
	if err := os.MkdirAll("/workspace", 0o755); err != nil {
		t.Skipf("/workspace unavailable: %v", err)
	}
	marker := filepath.Join("/workspace", "cell-export-test.txt")
	if err := os.WriteFile(marker, []byte("snap"), 0o644); err != nil {
		t.Skipf("/workspace not writable: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(marker) })

	resp := dispatch(guestproto.Request{Op: guestproto.OpExport})
	if !resp.OK {
		t.Fatalf("export: %s", resp.Error)
	}
	raw, err := base64.StdEncoding.DecodeString(resp.Tar)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 20 {
		t.Fatalf("tar too small: %d", len(raw))
	}
}
