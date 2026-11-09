package microvm

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// tarGzDir writes a gzip tar of dir (contents at archive root) to w.
func tarGzDir(dir string, w io.Writer) (err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("microvm: workspace %q is not a directory", abs)
	}
	gz := gzip.NewWriter(w)
	defer func() {
		if cerr := gz.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	tw := tar.NewWriter(gz)
	defer func() {
		if cerr := tw.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	walkErr := filepath.Walk(abs, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(abs, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, "..") {
			return fmt.Errorf("microvm: refusing path escape %q", path)
		}
		hdr, hdrErr := tar.FileInfoHeader(info, "")
		if hdrErr != nil {
			return hdrErr
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		_, copyErr := io.Copy(tw, f)
		if cerr := f.Close(); copyErr == nil {
			copyErr = cerr
		}
		return copyErr
	})
	if walkErr != nil {
		return walkErr
	}
	return nil
}
