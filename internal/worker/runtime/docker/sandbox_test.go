package docker

import (
	"strings"
	"testing"

	"github.com/YanChao1999/Cell/internal/worker/cache"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

func TestDockerRunArgsHardening(t *testing.T) {
	args, err := dockerRunArgs(runtime.CreateOpts{
		CellID:       "abc",
		Workspace:    "/tmp/cell-ws",
		Image:        "golang:1.23-bookworm",
		GitReposRoot: "/tmp/cell-repos",
		CacheMounts: []cache.Mount{{
			HostKey:   "go-mod",
			HostAbs:   "/tmp/cell-ws/.cell-cache/go-mod",
			Container: "/go/pkg/mod",
		}},
		Env: map[string]string{"HOME": "/workspace"},
	}, "none", "cell-abc")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\x00")
	want := []string{
		"--user",
		"--security-opt", "no-new-privileges:true",
		"--cap-drop", "ALL",
		"--read-only",
		"--tmpfs",
		"--network", "none",
		"/tmp/cell-repos:/tmp/cell-repos:ro",
		"/tmp/cell-ws/.cell-cache/go-mod:/go/pkg/mod",
		"golang:1.23-bookworm",
	}
	for _, w := range want {
		if !strings.Contains(joined, w) && !containsArg(args, w) {
			t.Fatalf("missing %q in args: %v", w, args)
		}
	}
	for _, banned := range []string{"--privileged", "docker.sock", "--pid=host", "--net=host"} {
		if containsArg(args, banned) || strings.Contains(joined, banned) {
			t.Fatalf("banned %q present in %v", banned, args)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.Contains(a, want) {
			return true
		}
	}
	return false
}

func TestDockerRunArgsRejectsRelativeRepos(t *testing.T) {
	_, err := dockerRunArgs(runtime.CreateOpts{
		CellID:       "x",
		Workspace:    "/tmp/ws",
		Image:        "golang:1.23-bookworm",
		GitReposRoot: "relative/repos",
	}, "", "cell-x")
	if err == nil {
		t.Fatal("expected error for relative GitReposRoot")
	}
}
