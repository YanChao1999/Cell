package docker

import (
	"strings"
	"testing"

	"github.com/YanChao1999/Cell/api"
)

func TestLimitArgsCPUMemory(t *testing.T) {
	args := limitArgs(api.Limits{CPU: "2", Memory: "4Gi"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--cpus 2") || !strings.Contains(joined, "--memory 4g") {
		t.Fatalf("%v", args)
	}
	if strings.Contains(joined, "storage-opt") {
		t.Fatalf("disk storage-opt must be opt-in: %v", args)
	}
}

func TestLimitArgsDiskOptIn(t *testing.T) {
	t.Setenv("CELL_DOCKER_DISK_STORAGE_OPT", "1")
	args := limitArgs(api.Limits{Disk: "10Gi"})
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--storage-opt size=10g") {
		t.Fatalf("%v", args)
	}
}

func TestDockerDisk(t *testing.T) {
	if got := dockerDisk("10Gi"); got != "10g" {
		t.Fatalf("%q", got)
	}
}
