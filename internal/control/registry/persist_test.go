package registry

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPersistReload(t *testing.T) {
	dir := t.TempDir()
	r1 := NewPersistent(dir, time.Minute)
	if _, err := r1.Register(RegisterRequest{ID: "w1", Endpoint: "http://127.0.0.1:9", MaxCells: 2, CacheKeys: []string{"go-mod"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := filepath.Abs(filepath.Join(dir, "workers.json")); err != nil {
		t.Fatal(err)
	}
	r2 := NewPersistent(dir, time.Minute)
	list := r2.List()
	if len(list) != 1 || list[0].ID != "w1" || list[0].Endpoint == "" {
		t.Fatalf("reload: %+v", list)
	}
}
