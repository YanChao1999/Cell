package microvm

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNameserversFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resolv.conf")
	content := "# comment\nnameserver 1.2.3.4\nnameserver not-an-ip\nnameserver 2001:db8::1\nsearch example.com\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := nameserversFromFile(path)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "1.2.3.4:53" || !strings.Contains(got[1], "2001:db8::1") {
		t.Fatalf("got %v", got)
	}
}

func TestDNSForwarderRoundTrip(t *testing.T) {
	// Upstream echo: reply with the same bytes (enough for this unit test).
	up, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = up.Close() }()
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := up.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = up.WriteTo(buf[:n], addr)
		}
	}()

	fwd, err := startDNSForwarderAddr("127.0.0.1:0", []string{up.LocalAddr().String()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fwd.Close() }()
	listen := fwd.conn.LocalAddr().String()

	c, err := net.DialTimeout("udp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	payload := []byte{0xab, 0xcd, 0x01, 0x00, 0x00, 0x01}
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatalf("got %x want %x", buf[:n], payload)
	}
}

func TestBootIPArgIncludesNameserver(t *testing.T) {
	n := &allowlistNet{guestIP: "172.26.3.2", hostIP: "172.26.3.1"}
	arg := n.bootIPArg()
	if !strings.Contains(arg, "cell.nameserver=172.26.3.1") {
		t.Fatalf("missing nameserver in %q", arg)
	}
	if !strings.Contains(arg, "ip=172.26.3.2::172.26.3.1") {
		t.Fatalf("missing ip= in %q", arg)
	}
}
