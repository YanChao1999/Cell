package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	cellapi "github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

// snapshotIDRe matches IDs from newSnapshotID (32 lowercase hex chars).
var snapshotIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s *Server) snapshotDir() (string, error) {
	base := s.meta.Dir()
	if base == "" {
		base = filepath.Join(os.TempDir(), "cell-snapshots")
	}
	dir := filepath.Join(base, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	cellID := r.PathValue("id")
	rec, ok := s.meta.Get(cellID)
	if !ok {
		writeErr(w, http.StatusNotFound, "cell not found")
		return
	}
	worker, ok := s.scheduler.Get(rec.WorkerID)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "worker not available")
		return
	}
	arch, ok := worker.(workerapi.WorkspaceArchive)
	if !ok {
		writeErr(w, http.StatusNotImplemented, "worker does not support workspace snapshots")
		return
	}
	dir, err := s.snapshotDir()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	snapID, err := newSnapshotID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	path, err := snapshotPath(dir, snapID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	f, err := os.Create(path)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := arch.PackWorkspace(r.Context(), cellID, f); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	st, _ := f.Stat()
	_ = f.Close()
	var size int64
	if st != nil {
		size = st.Size()
	}
	writeJSON(w, http.StatusCreated, cellapi.SnapshotInfo{
		ID:        snapID,
		CellID:    cellID,
		Bytes:     size,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) restoreSnapshot(ctx context.Context, worker workerapi.Worker, cellID, snapshotID string) error {
	if snapshotID == "" {
		return nil
	}
	arch, ok := worker.(workerapi.WorkspaceArchive)
	if !ok {
		return fmt.Errorf("worker does not support workspace snapshots")
	}
	dir, err := s.snapshotDir()
	if err != nil {
		return err
	}
	path, err := snapshotPath(dir, snapshotID)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("snapshot %s: %w", snapshotID, err)
	}
	defer func() { _ = f.Close() }()
	return arch.UnpackWorkspace(ctx, cellID, f)
}

func snapshotPath(dir, snapshotID string) (string, error) {
	if !validSnapshotID(snapshotID) {
		return "", fmt.Errorf("invalid snapshot_id")
	}
	cleanDir := filepath.Clean(dir)
	path := filepath.Join(cleanDir, snapshotID+".tar.gz")
	if filepath.Dir(path) != cleanDir {
		return "", fmt.Errorf("invalid snapshot_id")
	}
	return path, nil
}

func validSnapshotID(id string) bool {
	return snapshotIDRe.MatchString(id)
}

func newSnapshotID() (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(rand.Reader, b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
