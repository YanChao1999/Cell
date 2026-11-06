package git

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// withMirrorLock serializes clone/fetch for one mirror directory so concurrent
// Cell creates for the same repo do not race on git clone --mirror / fetch.
func withMirrorLock(mirrorDir string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(mirrorDir), 0o755); err != nil {
		return err
	}
	lockPath := mirrorDir + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("source/git: open mirror lock: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("source/git: flock mirror: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}
