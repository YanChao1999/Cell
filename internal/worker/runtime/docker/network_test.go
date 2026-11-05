package docker

import (
	"context"
	"testing"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

func TestNormalizeNetwork(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"default", "", false},
		{"open", "", false},
		{"bridge", "", false},
		{"NONE", "none", false},
		{"none", "none", false},
		{"allowlist", "allowlist", false},
		{"allowlist ", "allowlist", false},
		{"bogus", "", true},
	}
	for _, tc := range cases {
		got, err := normalizeNetwork(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("normalizeNetwork(%q): want error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("normalizeNetwork(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeNetwork(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestStubRejectsNoneAndAllowlist(t *testing.T) {
	s := NewStub()
	ctx := context.Background()
	if err := s.Create(ctx, runtime.CreateOpts{CellID: "c1", Network: "none"}); err == nil {
		t.Fatal("expected host stub to reject network none")
	}
	if err := s.Create(ctx, runtime.CreateOpts{CellID: "c2", Network: "allowlist"}); err == nil {
		t.Fatal("expected host stub to reject network allowlist")
	}
	if err := s.Create(ctx, runtime.CreateOpts{CellID: "c3", Network: "default"}); err != nil {
		t.Fatalf("default should be allowed on host stub: %v", err)
	}
}
