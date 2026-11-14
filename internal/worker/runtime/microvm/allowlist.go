package microvm

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/YanChao1999/Cell/internal/worker/egress"
	"github.com/YanChao1999/Cell/internal/worker/runtime"
)

// allowlistNet is the host-side TAP + cell-egress wiring for network: allowlist.
type allowlistNet struct {
	tapName  string
	hostIP   string // e.g. 172.26.7.1
	guestIP  string // e.g. 172.26.7.2
	guestMAC string
	cidr     int // /24
	proxy    *egress.Server
	dns      *dnsForwarder
	audit    *os.File
	proxyEnv map[string]string
}

// ipRun runs `ip` (overridable in tests).
var ipRun = func(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "ip", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ip %s: %w\n%s", strings.Join(args, " "), err, bytesTrim(out))
	}
	return nil
}

func bytesTrim(b []byte) string {
	return strings.TrimSpace(string(b))
}

func setupAllowlist(ctx context.Context, opts runtime.CreateOpts) (*allowlistNet, error) {
	octet := subnetOctet(opts.CellID)
	hostIP := fmt.Sprintf("172.26.%d.1", octet)
	guestIP := fmt.Sprintf("172.26.%d.2", octet)
	tapName := tapNameFor(opts.CellID)
	guestMAC := fmt.Sprintf("AA:FC:00:00:%02X:01", octet)

	auditDir := filepath.Join(opts.Workspace, ".cell")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return nil, err
	}
	auditPath := filepath.Join(auditDir, "egress-audit.jsonl")
	auditFile, err := egress.OpenAuditFile(auditPath)
	if err != nil {
		return nil, err
	}

	n := &allowlistNet{
		tapName:  tapName,
		hostIP:   hostIP,
		guestIP:  guestIP,
		guestMAC: guestMAC,
		cidr:     24,
		audit:    auditFile,
	}

	if err := ipRun(ctx, "tuntap", "add", "dev", tapName, "mode", "tap"); err != nil {
		_ = auditFile.Close()
		return nil, fmt.Errorf("microvm: create TAP %s (need CAP_NET_ADMIN): %w", tapName, err)
	}
	if err := ipRun(ctx, "addr", "add", fmt.Sprintf("%s/%d", hostIP, n.cidr), "dev", tapName); err != nil {
		_ = n.cleanup(context.Background())
		return nil, err
	}
	if err := ipRun(ctx, "link", "set", "dev", tapName, "up"); err != nil {
		_ = n.cleanup(context.Background())
		return nil, err
	}
	// Guest must not forward off-subnet; drop FORWARD from this TAP as belt-and-suspenders.
	_ = exec.CommandContext(ctx, "iptables", "-I", "FORWARD", "-i", tapName, "-j", "DROP").Run()

	listen := net.JoinHostPort(hostIP, "3128")
	upstream := egress.ResolveUpstream("")
	proxy, err := egress.StartProxy(ctx, egress.Policy{
		Mode:     "allowlist",
		Allow:    opts.EgressAllow,
		Upstream: upstream,
	}, listen, auditFile)
	if err != nil {
		_ = n.cleanup(context.Background())
		return nil, fmt.Errorf("microvm: start egress proxy: %w", err)
	}
	n.proxy = proxy
	dns, err := startDNSForwarder(hostIP, nil)
	if err != nil {
		_ = n.cleanup(context.Background())
		return nil, err
	}
	n.dns = dns
	proxyURL := "http://" + net.JoinHostPort(hostIP, "3128")
	n.proxyEnv = map[string]string{
		"HTTP_PROXY":  proxyURL,
		"HTTPS_PROXY": proxyURL,
		"http_proxy":  proxyURL,
		"https_proxy": proxyURL,
		"NO_PROXY":    "localhost,127.0.0.1," + hostIP,
		"no_proxy":    "localhost,127.0.0.1," + hostIP,
	}
	log.Printf("microvm: allowlist tap=%s host=%s guest=%s proxy=%s dns=%s:53 allow=%v",
		tapName, hostIP, guestIP, proxyURL, hostIP, opts.EgressAllow)
	return n, nil
}

func (n *allowlistNet) cleanup(ctx context.Context) error {
	if n == nil {
		return nil
	}
	if n.dns != nil {
		_ = n.dns.Close()
		n.dns = nil
	}
	if n.proxy != nil {
		_ = n.proxy.Close(ctx)
		n.proxy = nil
	}
	if n.audit != nil {
		_ = n.audit.Close()
		n.audit = nil
	}
	if n.tapName != "" {
		_ = exec.CommandContext(ctx, "iptables", "-D", "FORWARD", "-i", n.tapName, "-j", "DROP").Run()
		_ = ipRun(ctx, "link", "delete", n.tapName)
	}
	return nil
}

func tapNameFor(cellID string) string {
	safe := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, cellID)
	if len(safe) > 8 {
		safe = safe[:8]
	}
	name := "cmt-" + safe
	if len(name) > 15 { // IFNAMSIZ
		name = name[:15]
	}
	return name
}

func subnetOctet(cellID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(cellID))
	// 1..254 inclusive
	return int(h.Sum32()%254) + 1
}

// bootIPArg returns a Linux ip= kernel parameter for static addressing plus
// cell.nameserver=<hostIP> for the guest agent to seed /etc/resolv.conf.
func (n *allowlistNet) bootIPArg() string {
	if n == nil {
		return ""
	}
	// ip=<client-IP>::<gw-IP>:<netmask>:<hostname>:<device>:<autoconf>
	return fmt.Sprintf("ip=%s::%s:255.255.255.0:cell:eth0:off cell.nameserver=%s",
		n.guestIP, n.hostIP, n.hostIP)
}
