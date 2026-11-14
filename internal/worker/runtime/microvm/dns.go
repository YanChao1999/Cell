package microvm

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/YanChao1999/Cell/internal/worker/runtime/microvm/guestproto"
)

// dnsForwarder is a tiny UDP DNS proxy bound to the TAP host IP.
// Guests point resolv.conf at this address; queries are relayed to the
// worker host's upstream resolvers (from /etc/resolv.conf).
type dnsForwarder struct {
	conn      net.PacketConn
	upstreams []string // host:53
	closed    chan struct{}
	wg        sync.WaitGroup
}

func startDNSForwarder(listenIP string, upstreams []string) (*dnsForwarder, error) {
	return startDNSForwarderAddr(net.JoinHostPort(listenIP, "53"), upstreams)
}

func startDNSForwarderAddr(addr string, upstreams []string) (*dnsForwarder, error) {
	if len(upstreams) == 0 {
		upstreams = hostNameservers()
	}
	if len(upstreams) == 0 {
		upstreams = []string{"1.1.1.1:53", "8.8.8.8:53"}
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("microvm: DNS listen %s (need CAP_NET_BIND_SERVICE or root for :53): %w", addr, err)
	}
	f := &dnsForwarder{conn: pc, upstreams: upstreams, closed: make(chan struct{})}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		f.serve()
	}()
	log.Printf("microvm: DNS forwarder on %s → %v", addr, upstreams)
	return f, nil
}

func (f *dnsForwarder) Close() error {
	if f == nil {
		return nil
	}
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	err := f.conn.Close()
	f.wg.Wait()
	return err
}

func (f *dnsForwarder) serve() {
	buf := make([]byte, 4096)
	for {
		_ = f.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, client, err := f.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-f.closed:
				return
			default:
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go f.relay(pkt, client)
	}
}

func (f *dnsForwarder) relay(query []byte, client net.Addr) {
	var last error
	for _, up := range f.upstreams {
		resp, err := dnsExchange(up, query, 2*time.Second)
		if err != nil {
			last = err
			continue
		}
		_, _ = f.conn.WriteTo(resp, client)
		return
	}
	if last != nil {
		log.Printf("microvm: DNS forward failed: %v", last)
	}
}

func dnsExchange(upstream string, query []byte, timeout time.Duration) ([]byte, error) {
	c, err := net.DialTimeout("udp", upstream, timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, buf[:n])
	return out, nil
}

func hostNameservers() []string {
	return nameserversFromFile("/etc/resolv.conf")
}

func nameserversFromFile(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			ip := fields[1]
			if net.ParseIP(ip) == nil {
				continue
			}
			out = append(out, net.JoinHostPort(ip, "53"))
		}
	}
	return out
}

// applyGuestNameserver writes /etc/resolv.conf inside the guest via the agent.
func (c *fcCell) applyGuestNameserver(ctx context.Context, nameserver string) error {
	if c == nil || nameserver == "" {
		return nil
	}
	cmd := fmt.Sprintf("printf 'nameserver %s\\n' > /etc/resolv.conf", nameserver)
	resp, err := c.roundTrip(ctx, guestproto.Request{
		Op:      guestproto.OpExec,
		Command: cmd,
		Timeout: 5,
		Cwd:     "/",
	}, 10*time.Second)
	if err != nil {
		return err
	}
	if !resp.OK || resp.ExitCode != 0 {
		return fmt.Errorf("microvm: write resolv.conf: ok=%v exit=%d err=%s stderr=%s",
			resp.OK, resp.ExitCode, resp.Error, resp.Stderr)
	}
	return nil
}
