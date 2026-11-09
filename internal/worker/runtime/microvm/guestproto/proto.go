// Package guestproto is the vsock JSON protocol between host microVM Runtime and cell-guest-agent.
package guestproto

import "time"

const (
	// DefaultPort is the vsock port the guest agent listens on.
	DefaultPort = 5032
	// MaxFrameBytes caps a single request/response frame (workspace sync chunks inclusive).
	MaxFrameBytes = 64 << 20
)

// Op names.
const (
	OpPing     = "ping"
	OpSync     = "sync_workspace"
	OpExec     = "exec"
	OpShutdown = "shutdown"
)

// Request is one framed JSON message from host → guest.
type Request struct {
	Op      string            `json:"op"`
	Command string            `json:"command,omitempty"`
	Timeout int               `json:"timeout_sec,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	// Tar is base64-encoded tar.gz for OpSync (M1; small workspaces).
	Tar string `json:"tar_b64,omitempty"`
}

// Response is one framed JSON message from guest → host.
type Response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// DefaultExecTimeout when Request.Timeout is 0.
const DefaultExecTimeout = 120 * time.Second
