// Command cell-guest-agent runs inside a microVM guest and serves exec/sync over vsock.
//
// Boot with: init=/usr/local/bin/cell-guest-agent (or systemd unit).
// Listens on vsock port guestproto.DefaultPort (5032).
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YanChao1999/Cell/internal/worker/runtime/microvm/guestproto"
	"github.com/mdlayher/vsock"
)

func main() {
	_ = os.MkdirAll("/workspace", 0o755)
	port := uint32(guestproto.DefaultPort)
	ln, err := vsock.Listen(port, nil)
	if err != nil {
		// Fallback: TCP on localhost for rootfs bring-up / unit tests without vsock.
		log.Printf("cell-guest-agent: vsock listen failed (%v); trying TCP :%d", err, port)
		lnTCP, err2 := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err2 != nil {
			log.Fatalf("listen: vsock=%v tcp=%v", err, err2)
		}
		serve(lnTCP)
		return
	}
	log.Printf("cell-guest-agent: listening vsock port %d", port)
	serve(ln)
}

func serve(ln net.Listener) {
	defer func() { _ = ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Minute))
	var req guestproto.Request
	if err := readFrame(conn, &req); err != nil {
		log.Printf("read: %v", err)
		return
	}
	resp := dispatch(req)
	if err := writeFrame(conn, resp); err != nil {
		log.Printf("write: %v", err)
	}
	if req.Op == guestproto.OpShutdown {
		go func() {
			time.Sleep(100 * time.Millisecond)
			os.Exit(0)
		}()
	}
}

func dispatch(req guestproto.Request) guestproto.Response {
	switch req.Op {
	case guestproto.OpPing:
		return guestproto.Response{OK: true}
	case guestproto.OpSync:
		if err := syncWorkspace(req.Tar); err != nil {
			return guestproto.Response{OK: false, Error: err.Error()}
		}
		return guestproto.Response{OK: true}
	case guestproto.OpExec:
		return runExec(req)
	case guestproto.OpShutdown:
		return guestproto.Response{OK: true}
	default:
		return guestproto.Response{OK: false, Error: "unknown op"}
	}
}

func syncWorkspace(tarB64 string) error {
	raw, err := base64.StdEncoding.DecodeString(tarB64)
	if err != nil {
		return err
	}
	_ = os.RemoveAll("/workspace")
	if err := os.MkdirAll("/workspace", 0o755); err != nil {
		return err
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer func() { _ = gr.Close() }()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join("/workspace", filepath.Clean(hdr.Name))
		if target != "/workspace" && !strings.HasPrefix(target, "/workspace/") {
			return fmt.Errorf("refusing escape %q", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

func runExec(req guestproto.Request) guestproto.Response {
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout <= 0 {
		timeout = guestproto.DefaultExecTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cwd := req.Cwd
	if cwd == "" {
		cwd = "/workspace"
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", req.Command)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	resp := guestproto.Response{OK: true, Stdout: stdout.String(), Stderr: stderr.String()}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			resp.ExitCode = ee.ExitCode()
		} else if ctx.Err() != nil {
			resp.OK = false
			resp.Error = "timeout"
			resp.ExitCode = -1
		} else {
			resp.OK = false
			resp.Error = err.Error()
			resp.ExitCode = -1
		}
	}
	return resp
}

// local copies of frame helpers to avoid importing the host microvm package's vsock dial.
func writeFrame(w io.Writer, v any) error {
	return guestproto.WriteFrame(w, v)
}
func readFrame(r io.Reader, dest any) error {
	return guestproto.ReadFrame(r, dest)
}
