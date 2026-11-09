package guestproto

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Request{Op: OpPing}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	var out Request
	if err := ReadFrame(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if out.Op != OpPing {
		t.Fatalf("got %+v", out)
	}
}
