package version

import (
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	s := String("celld")
	if !strings.HasPrefix(s, "celld ") {
		t.Fatalf("got %q", s)
	}
	if Version == "" {
		t.Fatal("empty Version")
	}
}
