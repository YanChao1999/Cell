package egress

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Server is an HTTP/HTTPS (CONNECT) forward proxy that only dials allowlisted hosts.
// When Upstream is set, dial-out goes through that HTTP proxy after allowlist checks.
type Server struct {
	policy    Policy
	audit     io.Writer
	http      *http.Server
	ln        net.Listener
	addr      string
	upstream  *url.URL
	transport *http.Transport
	mu        sync.Mutex
}

type auditEvent struct {
	Time   string `json:"time"`
	Method string `json:"method"`
	Host   string `json:"host"`
	Action string `json:"action"` // allow | deny
	Reason string `json:"reason,omitempty"`
}

// StartProxy starts an allowlist proxy on listenAddr (e.g. "127.0.0.1:0" or ":3128").
func StartProxy(ctx context.Context, p Policy, listenAddr string, audit io.Writer) (*Server, error) {
	_ = ctx
	p.Mode = strings.TrimSpace(strings.ToLower(p.Mode))
	if p.Mode == "" {
		p.Mode = "allowlist"
	}
	if p.Mode != "allowlist" {
		return nil, fmt.Errorf("egress: unsupported mode %q", p.Mode)
	}
	allows := normalizeAllow(p.Allow)
	if len(allows) == 0 {
		return nil, fmt.Errorf("egress: allowlist is empty")
	}
	p.Allow = allows
	if audit == nil {
		audit = io.Discard
	}

	var upstream *url.URL
	if u := strings.TrimSpace(p.Upstream); u != "" {
		parsed, err := url.Parse(u)
		if err != nil {
			return nil, fmt.Errorf("egress: upstream: %w", err)
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return nil, fmt.Errorf("egress: upstream scheme must be http or https, got %q", parsed.Scheme)
		}
		if parsed.Host == "" {
			return nil, fmt.Errorf("egress: upstream missing host")
		}
		upstream = parsed
		p.Upstream = parsed.String()
	}

	s := &Server{policy: p, audit: audit, upstream: upstream}
	// Never use ProxyFromEnvironment: Cell env points at us; ambient proxy on the
	// sidecar is folded into Policy.Upstream only at process start.
	s.transport = &http.Transport{
		Proxy:               s.proxyFunc,
		DialContext:         s.dialContext,
		TLSHandshakeTimeout: 30 * time.Second,
		ForceAttemptHTTP2:   false,
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.addr = ln.Addr().String()
	s.http = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 30 * time.Second,
	}
	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("egress: proxy serve: %v", err)
		}
	}()
	return s, nil
}

func (s *Server) proxyFunc(r *http.Request) (*url.URL, error) {
	if s.upstream == nil {
		return nil, nil
	}
	return s.upstream, nil
}

func (s *Server) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	_ = network
	d := net.Dialer{Timeout: 30 * time.Second}
	return d.DialContext(ctx, "tcp", address)
}

// Addr returns host:port for proxy URLs.
func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	return s.addr
}

// Upstream returns the configured upstream proxy URL, or empty.
func (s *Server) Upstream() string {
	if s == nil || s.upstream == nil {
		return ""
	}
	return s.upstream.String()
}

// Close stops the proxy.
func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.http == nil {
		return nil
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleCONNECT(w, r)
		return
	}
	s.handleHTTP(w, r)
}

func (s *Server) handleCONNECT(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if !s.allowedHostPort(host) {
		s.logAudit("CONNECT", host, "deny", "host not allowlisted")
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	dest, err := s.dial(host)
	if err != nil {
		s.logAudit("CONNECT", host, "deny", err.Error())
		http.Error(w, "dial failed", http.StatusBadGateway)
		return
	}
	hij, ok := w.(http.Hijacker)
	if !ok {
		_ = dest.Close()
		http.Error(w, "hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hij.Hijack()
	if err != nil {
		_ = dest.Close()
		return
	}
	s.logAudit("CONNECT", host, "allow", "")
	_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	bidirectionalCopy(client, dest)
}

func (s *Server) handleHTTP(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		http.Error(w, "absolute URL required", http.StatusBadRequest)
		return
	}
	host := r.URL.Host
	if host == "" {
		host = r.Host
	}
	if !s.allowedHostPort(host) {
		s.logAudit(r.Method, host, "deny", "host not allowlisted")
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	s.logAudit(r.Method, host, "allow", "")
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	resp, err := s.transport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) dial(hostPort string) (net.Conn, error) {
	host, port, err := splitHostPort(hostPort)
	if err != nil {
		return nil, err
	}
	if err := s.checkHost(host); err != nil {
		return nil, err
	}
	dest := net.JoinHostPort(host, port)
	if s.upstream != nil {
		return dialViaHTTPProxy(s.upstream, dest, 30*time.Second)
	}
	return net.DialTimeout("tcp", dest, 30*time.Second)
}

// dialViaHTTPProxy opens a TCP tunnel to dest through an HTTP CONNECT proxy.
func dialViaHTTPProxy(proxy *url.URL, dest string, timeout time.Duration) (net.Conn, error) {
	proxyAddr, err := proxyDialAddr(proxy)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", proxyAddr, timeout)
	if err != nil {
		return nil, fmt.Errorf("upstream dial: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))

	var b strings.Builder
	fmt.Fprintf(&b, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", dest, dest)
	if proxy.User != nil {
		pass, _ := proxy.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(proxy.User.Username() + ":" + pass))
		fmt.Fprintf(&b, "Proxy-Authorization: Basic %s\r\n", token)
	}
	b.WriteString("\r\n")
	if _, err := io.WriteString(conn, b.String()); err != nil {
		_ = conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream CONNECT: %w", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream CONNECT status %d", resp.StatusCode)
	}
	// Buffered bytes (if any) stay in br; for CONNECT 200 the body is empty.
	if br.Buffered() > 0 {
		_ = conn.Close()
		return nil, fmt.Errorf("upstream CONNECT: unexpected buffered data")
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func proxyDialAddr(u *url.URL) (string, error) {
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		switch u.Scheme {
		case "https":
			port = "443"
		default:
			port = "80"
		}
	}
	if host == "" {
		return "", fmt.Errorf("upstream missing host")
	}
	return net.JoinHostPort(host, port), nil
}

func (s *Server) allowedHostPort(hostPort string) bool {
	host, _, err := splitHostPort(hostPort)
	if err != nil {
		host = hostPort
	}
	return s.checkHost(host) == nil
}

func (s *Server) checkHost(host string) error {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return fmt.Errorf("empty host")
	}
	// Reject raw IPs unless explicitly allowlisted (same matcher).
	if ip := net.ParseIP(host); ip != nil {
		if matchAllow(host, s.policy.Allow) {
			return nil
		}
		return fmt.Errorf("ip literal not allowlisted")
	}
	if !matchAllow(host, s.policy.Allow) {
		return fmt.Errorf("host not allowlisted")
	}
	return nil
}

func matchAllow(host string, allows []string) bool {
	for _, a := range allows {
		if host == a {
			return true
		}
		if strings.HasPrefix(a, ".") && strings.HasSuffix(host, a) {
			return true
		}
		// subdomain of allowlisted apex: foo.github.com matches github.com
		if strings.HasSuffix(host, "."+a) {
			return true
		}
	}
	return false
}

func normalizeAllow(in []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, a := range in {
		a = strings.ToLower(strings.TrimSpace(a))
		a = strings.TrimPrefix(a, "https://")
		a = strings.TrimPrefix(a, "http://")
		if i := strings.IndexByte(a, '/'); i >= 0 {
			a = a[:i]
		}
		if i := strings.IndexByte(a, ':'); i >= 0 {
			a = a[:i]
		}
		if a == "" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	return out
}

func splitHostPort(hostPort string) (string, string, error) {
	if strings.HasPrefix(hostPort, "[") {
		return net.SplitHostPort(hostPort)
	}
	if !strings.Contains(hostPort, ":") {
		return hostPort, "80", nil
	}
	return net.SplitHostPort(hostPort)
}

func (s *Server) logAudit(method, host, action, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev := auditEvent{
		Time:   time.Now().UTC().Format(time.RFC3339Nano),
		Method: method,
		Host:   host,
		Action: action,
		Reason: reason,
	}
	b, _ := json.Marshal(ev)
	_, _ = s.audit.Write(append(b, '\n'))
}

func bidirectionalCopy(a, b net.Conn) {
	defer func() { _ = a.Close() }()
	defer func() { _ = b.Close() }()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
}

// OpenAuditFile creates/appends an audit log file.
func OpenAuditFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

// ResolveUpstream picks the corporate/upstream proxy URL for the sidecar only.
// Priority: explicit > CELL_EGRESS_UPSTREAM > HTTPS_PROXY > HTTP_PROXY
// (lowercase variants of the proxy env names are also checked).
func ResolveUpstream(explicit string) string {
	if u := strings.TrimSpace(explicit); u != "" {
		return u
	}
	for _, k := range []string{"CELL_EGRESS_UPSTREAM", "HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if u := strings.TrimSpace(os.Getenv(k)); u != "" {
			return u
		}
	}
	return ""
}

// RewriteUpstreamLoopback rewrites 127.0.0.1/localhost in an upstream URL to
// host.docker.internal so a sidecar on the Docker bridge can reach a proxy on the host.
// Returns the (possibly rewritten) URL and whether the proxy container needs
// --add-host=host.docker.internal:host-gateway.
func RewriteUpstreamLoopback(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return raw, false
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	u.Host = net.JoinHostPort("host.docker.internal", port)
	return u.String(), true
}
