package microvm

import (
	"io"

	"github.com/YanChao1999/Cell/internal/worker/runtime/microvm/guestproto"
)

func writeFrame(w io.Writer, v any) error { return guestproto.WriteFrame(w, v) }
func readFrame(r io.Reader, dest any) error {
	return guestproto.ReadFrame(r, dest)
}
