package microvm

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// vmmLaunch describes how to start Firecracker (bare or jailed).
type vmmLaunch struct {
	Cmd       *exec.Cmd
	APISock   string // host-visible API UNIX socket
	KernelAPI string // path passed to Firecracker boot-source API
	RootfsAPI string // path passed to Firecracker drives API
	JailRoot  string // non-empty when jailed (chroot dir on host)
	VsockHost string // host UDS for vsock
}

// allowUnjailed reports whether bare firecracker (no jailer) is permitted.
func allowUnjailed() bool {
	v := strings.TrimSpace(os.Getenv("CELL_MICROVM_ALLOW_UNJAILED"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// resolveJailer returns the jailer binary path, or empty if unjailed is allowed.
func resolveJailer(cfg Config) (string, error) {
	j := strings.TrimSpace(cfg.Jailer)
	if j == "" {
		j = "jailer"
	}
	path, err := lookPath(j)
	if err == nil {
		return path, nil
	}
	if allowUnjailed() {
		return "", nil
	}
	return "", fmt.Errorf("microvm: jailer %q not found (set CELL_MICROVM_JAILER or CELL_MICROVM_ALLOW_UNJAILED=1 for non-prod): %w", j, err)
}

// prepareVMM builds the Firecracker process, optionally wrapped by jailer.
//
// When jailed, kernel/rootfs are hard-linked (or copied) into the jail directory
// and API resource paths are chroot-relative ("/vmlinux", "/rootfs.ext4").
func prepareVMM(cfg Config, cellID, workDir, vsockHost string) (*vmmLaunch, error) {
	vmmPath, err := lookPath(cfg.VMM)
	if err != nil {
		return nil, err
	}
	jailerPath, err := resolveJailer(cfg)
	if err != nil {
		return nil, err
	}

	apiSock := filepath.Join(workDir, "firecracker.sock")
	_ = os.Remove(apiSock)

	if jailerPath == "" {
		return &vmmLaunch{
			Cmd:       exec.Command(vmmPath, "--api-sock", apiSock),
			APISock:   apiSock,
			KernelAPI: cfg.Kernel,
			RootfsAPI: cfg.Rootfs,
			VsockHost: vsockHost,
		}, nil
	}

	id := sanitizeID(cellID)
	if id == "" {
		id = "cell"
	}
	chrootBase := filepath.Dir(workDir)
	jailRoot := filepath.Join(chrootBase, id)
	if err := os.MkdirAll(jailRoot, 0o755); err != nil {
		return nil, err
	}
	if err := linkOrCopy(cfg.Kernel, filepath.Join(jailRoot, "vmlinux")); err != nil {
		return nil, fmt.Errorf("microvm: stage kernel into jail: %w", err)
	}
	if err := linkOrCopy(cfg.Rootfs, filepath.Join(jailRoot, "rootfs.ext4")); err != nil {
		return nil, fmt.Errorf("microvm: stage rootfs into jail: %w", err)
	}
	apiRel := "firecracker.sock"
	apiSock = filepath.Join(jailRoot, apiRel)
	_ = os.Remove(apiSock)

	uid := os.Getuid()
	gid := os.Getgid()
	if uid == 0 {
		uid = 123
		gid = 100
	}
	cmd := exec.Command(jailerPath,
		"--id", id,
		"--exec-file", vmmPath,
		"--uid", strconv.Itoa(uid),
		"--gid", strconv.Itoa(gid),
		"--chroot-base-dir", chrootBase,
		"--",
		"--api-sock", "/"+apiRel,
	)
	return &vmmLaunch{
		Cmd:       cmd,
		APISock:   apiSock,
		KernelAPI: "/vmlinux",
		RootfsAPI: "/rootfs.ext4",
		JailRoot:  jailRoot,
		VsockHost: vsockHost,
	}, nil
}

func linkOrCopy(src, dst string) error {
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}
