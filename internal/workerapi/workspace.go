package workerapi

import (
	"context"
	"io"
)

// WorkspaceArchive packs/unpacks a Cell's host workspace (snapshot support).
type WorkspaceArchive interface {
	PackWorkspace(ctx context.Context, cellID string, w io.Writer) error
	UnpackWorkspace(ctx context.Context, cellID string, r io.Reader) error
}
