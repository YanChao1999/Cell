package cache

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// Default seed limits keep create latency bounded.
	defaultMaxFiles = 50000
	defaultMaxBytes = 2 << 30 // 2 GiB
)

// SeedOptions controls trusted copy from a shared durable cache into a per-Cell dir.
type SeedOptions struct {
	MaxFiles int
	MaxBytes int64
}

// SeedCopy copies shared → dest as a trusted warm seed.
// Security properties:
//   - never uses hardlinks (Cell writes must not mutate shared)
//   - refuses symlinks (shared must not point outside the cache tree)
//   - copies regular files and directories only
func SeedCopy(shared, dest string, opt SeedOptions) error {
	if shared == "" || dest == "" {
		return fmt.Errorf("cache: seed requires shared and dest paths")
	}
	sharedAbs, err := filepath.Abs(shared)
	if err != nil {
		return err
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	if sharedAbs == destAbs {
		return fmt.Errorf("cache: seed shared and dest are the same path")
	}
	st, err := os.Stat(sharedAbs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("cache: shared seed is not a directory: %s", sharedAbs)
	}
	if err := os.MkdirAll(destAbs, 0o755); err != nil {
		return err
	}
	if opt.MaxFiles <= 0 {
		opt.MaxFiles = defaultMaxFiles
	}
	if opt.MaxBytes <= 0 {
		opt.MaxBytes = defaultMaxBytes
	}

	var files int
	var bytes int64
	return filepath.Walk(sharedAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(sharedAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Block path escape.
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("cache: refusing seed path escape %q", rel)
		}
		target := filepath.Join(destAbs, rel)

		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			return fmt.Errorf("cache: refusing symlink in shared cache: %s", path)
		}
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !mode.IsRegular() {
			return nil // skip devices/sockets
		}
		files++
		if files > opt.MaxFiles {
			return fmt.Errorf("cache: seed exceeded max files (%d)", opt.MaxFiles)
		}
		bytes += info.Size()
		if bytes > opt.MaxBytes {
			return fmt.Errorf("cache: seed exceeded max bytes (%d)", opt.MaxBytes)
		}
		return copyFile(path, target, mode.Perm())
	})
}

func copyFile(src, dst string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
