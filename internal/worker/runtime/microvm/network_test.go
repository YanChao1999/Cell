package microvm

import (
	"strings"
	"testing"

	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

func TestNormalizeNetwork(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", networkNone, false},
		{"none", networkNone, false},
		{"allowlist", networkAllowlist, false},
		{"default", "", true},
		{"open", "", true},
		{"bridge", "", true},
		{"weird", "", true},
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

func TestValidateNetworkOptsAllowlist(t *testing.T) {
	_, err := validateNetworkOpts(runtime.CreateOpts{Network: "allowlist"})
	if err == nil || !strings.Contains(err.Error(), "egress.allow") {
		t.Fatalf("want egress.allow error, got %v", err)
	}
	_, err = validateNetworkOpts(runtime.CreateOpts{
		Network:     "allowlist",
		EgressAllow: []string{"example.com"},
	})
	if err == nil || !strings.Contains(err.Error(), "workspace") {
		t.Fatalf("want workspace error, got %v", err)
	}
	mode, err := validateNetworkOpts(runtime.CreateOpts{
		Network:     "allowlist",
		EgressAllow: []string{"example.com"},
		Workspace:   "/tmp/ws",
	})
	if err != nil || mode != networkAllowlist {
		t.Fatalf("got mode=%q err=%v", mode, err)
	}
}

func TestCreateRejectsOpenNetwork(t *testing.T) {
	t.Setenv("CELL_MICROVM_ALLOW_UNJAILED", "1")
	lookPath = func(file string) (string, error) { return "/usr/bin/" + file, nil }
	t.Cleanup(func() { lookPath = execLookPathStd })

	dir := t.TempDir()
	r := NewWith(Config{VMM: "firecracker", ArtifactDir: dir}, NewFirecracker())
	err := r.Create(t.Context(), runtime.CreateOpts{CellID: "c1", Network: "default"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("want reject default, got %v", err)
	}
}
