package egress

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Server is an HTTP/HTTPS (CONNECT) forward proxy that only dials allowlisted hosts.
type Server struct {
	policy Policy
	audit  io.Writer
	http   *http.Server
	ln     net.Listener
	addr   string
	mu     sync.Mutex
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
	s := &Server{policy: p, audit: audit}
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

// Addr returns host:port for proxy URLs.
func (s *Server) Addr() string {
	if s == nil {
		return ""
	}
	return s.addr
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
	resp, err := http.DefaultTransport.RoundTrip(outReq)
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
	return net.DialTimeout("tcp", net.JoinHostPort(host, port), 30*time.Second)
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
