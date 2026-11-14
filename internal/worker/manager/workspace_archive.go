package manager

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/YanChao1999/Cell/internal/workerapi"
)

// PackWorkspace writes a gzip tar of the Cell host workspace to w.
// For microVM, guest /workspace is pulled to the host first so agent edits are captured.
func (lw *LocalWorker) PackWorkspace(ctx context.Context, cellID string, w io.Writer) error {
	cell, err := lw.getCell(cellID)
	if err != nil {
		return err
	}
	if puller, ok := lw.rt.(workspacePuller); ok {
		if err := puller.PullWorkspace(ctx, cellID, cell.Workspace); err != nil {
			return err
		}
	}
	return tarGzDir(cell.Workspace, w)
}

// UnpackWorkspace replaces the Cell host workspace with a gzip tar.
// Existing contents are wiped first so restore does not merge leftovers.
// When the runtime supports it (microVM), the guest workspace is re-synced.
func (lw *LocalWorker) UnpackWorkspace(ctx context.Context, cellID string, r io.Reader) error {
	cell, err := lw.getCell(cellID)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(cell.Workspace); err != nil {
		return fmt.Errorf("workspace archive: clear %s: %w", cell.Workspace, err)
	}
	if err := os.MkdirAll(cell.Workspace, 0o755); err != nil {
		return err
	}
	if err := untarGzDir(r, cell.Workspace); err != nil {
		return err
	}
	if rs, ok := lw.rt.(workspaceResyncer); ok {
		return rs.ResyncWorkspace(ctx, cellID, cell.Workspace)
	}
	return nil
}

type workspaceResyncer interface {
	ResyncWorkspace(ctx context.Context, cellID, workspace string) error
}

type workspacePuller interface {
	PullWorkspace(ctx context.Context, cellID, workspace string) error
}

func (lw *LocalWorker) getCell(cellID string) (*workerapi.Cell, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	c := lw.cells[cellID]
	if c == nil {
		return nil, fmt.Errorf("worker: cell %s not found", cellID)
	}
	return c, nil
}

func tarGzDir(root string, w io.Writer) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
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
		rel = filepath.ToSlash(rel)
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if info.IsDir() {
			hdr.Name += "/"
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		_ = f.Close()
		return copyErr
	})
	if err != nil {
		_ = tw.Close()
		_ = gw.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		return err
	}
	return gw.Close()
}

func untarGzDir(r io.Reader, dest string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	gr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(hdr.Name)
		if name == "." || strings.HasPrefix(name, "..") {
			continue
		}
		target := filepath.Join(dest, name)
		if !strings.HasPrefix(target, filepath.Clean(dest)+string(os.PathSeparator)) && target != filepath.Clean(dest) {
			return fmt.Errorf("workspace archive: path escapes dest: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			_ = f.Close()
			if copyErr != nil {
				return copyErr
			}
		}
	}
}
