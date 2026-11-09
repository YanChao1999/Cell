package microvm

import (
	"fmt"
	"net"
	"time"
)

// dialHostVsock connects to a Firecracker host-side vsock UDS and CONNECT to guestPort.
// See https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md
func dialHostVsock(udsPath string, guestPort uint32, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.Dial("unix", udsPath)
	if err != nil {
		return nil, fmt.Errorf("microvm: vsock uds: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", guestPort); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("microvm: vsock CONNECT: %w", err)
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("microvm: vsock CONNECT reply: %w", err)
	}
	reply := string(buf[:n])
	if len(reply) < 2 || reply[:2] != "OK" {
		_ = conn.Close()
		return nil, fmt.Errorf("microvm: vsock CONNECT rejected: %q", reply)
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}
