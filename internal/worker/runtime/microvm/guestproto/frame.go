package guestproto

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// WriteFrame writes a length-prefixed JSON message (uint32 BE + payload).
func WriteFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxFrameBytes {
		return fmt.Errorf("guestproto: frame too large (%d)", len(b))
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// ReadFrame reads one length-prefixed JSON message into dest.
func ReadFrame(r io.Reader, dest any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 || n > MaxFrameBytes {
		return fmt.Errorf("guestproto: bad frame length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, dest)
}
