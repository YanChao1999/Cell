package git

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithMirrorLockSerializes(t *testing.T) {
	dir := t.TempDir()
	mirror := filepath.Join(dir, "abc")
	var concurrent atomic.Int32
	var max atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := withMirrorLock(mirror, func() error {
				n := concurrent.Add(1)
				for {
					cur := max.Load()
					if n <= cur || max.CompareAndSwap(cur, n) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				concurrent.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("lock: %v", err)
			}
		}()
	}
	wg.Wait()
	if max.Load() != 1 {
		t.Fatalf("expected exclusive lock, saw concurrency %d", max.Load())
	}
}
