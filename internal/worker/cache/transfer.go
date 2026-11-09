package cache

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultTransferMaxFiles = 50000
	defaultTransferMaxBytes = 2 << 30 // 2 GiB
)

// TransferOptions bounds pack/unpack size.
type TransferOptions struct {
	MaxFiles int
	MaxBytes int64
}

func (o TransferOptions) withDefaults() TransferOptions {
	if o.MaxFiles <= 0 {
		o.MaxFiles = defaultTransferMaxFiles
	}
	if o.MaxBytes <= 0 {
		o.MaxBytes = defaultTransferMaxBytes
	}
	return o
}

// DirIsWarm reports whether path exists and contains at least one regular file.
func DirIsWarm(path string) bool {
	if path == "" {
		return false
	}
	warm := false
	_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.Mode().IsRegular() && info.Size() >= 0 {
			warm = true
			return filepath.SkipAll
		}
		return nil
	})
	return warm
}

// WarmKeys returns host keys under the layout that currently hold files.
func (l *Layout) WarmKeys() []string {
	if l == nil || l.Root == "" {
		return nil
	}
	entries, err := os.ReadDir(l.Root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		key := e.Name()
		if _, err := sanitizeHostKey(key); err != nil {
			continue
		}
		if DirIsWarm(filepath.Join(l.Root, key)) {
			out = append(out, key)
		}
	}
	return out
}

// PackTarGzip writes a gzip-compressed tar of the host key directory to w.
// Symlinks and paths escaping the key root are refused.
func (l *Layout) PackTarGzip(w io.Writer, hostKey string, opt TransferOptions) error {
	opt = opt.withDefaults()
	key, err := sanitizeHostKey(hostKey)
	if err != nil {
		return err
	}
	if l == nil || l.Root == "" {
		return fmt.Errorf("cache: empty cache root")
	}
	root := filepath.Join(l.Root, key)
	st, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("cache: %q is not a directory", key)
	}

	gz := gzip.NewWriter(w)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	var files int
	var bytes int64
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("cache: refusing path escape %q", rel)
		}
		// Symlinks are never packed — same policy as SeedCopy.
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache: refusing symlink %q", rel)
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
			return tw.WriteHeader(hdr)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		files++
		if files > opt.MaxFiles {
			return fmt.Errorf("cache: transfer exceeds max files (%d)", opt.MaxFiles)
		}
		bytes += info.Size()
		if bytes > opt.MaxBytes {
			return fmt.Errorf("cache: transfer exceeds max bytes (%d)", opt.MaxBytes)
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.Copy(tw, f)
		return err
	})
}

// UnpackTarGzip extracts a gzip tar into the host key directory (created if needed).
// Existing files are overwritten; symlinks in the archive are refused.
func (l *Layout) UnpackTarGzip(r io.Reader, hostKey string, opt TransferOptions) error {
	opt = opt.withDefaults()
	key, err := sanitizeHostKey(hostKey)
	if err != nil {
		return err
	}
	if l == nil || l.Root == "" {
		return fmt.Errorf("cache: empty cache root")
	}
	dest := filepath.Join(l.Root, key)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)

	var files int
	var bytes int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || name == "" {
			continue
		}
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("cache: refusing archive path %q", hdr.Name)
		}
		target := filepath.Join(destAbs, name)
		// Ensure still under destAbs after Join.
		if !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) && target != destAbs {
			return fmt.Errorf("cache: refusing archive escape %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			files++
			if files > opt.MaxFiles {
				return fmt.Errorf("cache: transfer exceeds max files (%d)", opt.MaxFiles)
			}
			bytes += hdr.Size
			if bytes > opt.MaxBytes {
				return fmt.Errorf("cache: transfer exceeds max bytes (%d)", opt.MaxBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("cache: refusing link in archive %q", hdr.Name)
		default:
			// skip other types
		}
	}
}
