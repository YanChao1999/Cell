package egress

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMatchAllow(t *testing.T) {
	allows := []string{"github.com", "proxy.golang.org"}
	if !matchAllow("github.com", allows) {
		t.Fatal("exact")
	}
	if !matchAllow("api.github.com", allows) {
		t.Fatal("subdomain")
	}
	if matchAllow("evil.com", allows) {
		t.Fatal("should deny")
	}
}

func TestProxyAllowAndDeny(t *testing.T) {
	// Backend for allowed host via CONNECT is awkward in unit tests; exercise HTTP proxy path.
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = upstream.Close() }()
	go func() {
		_ = http.Serve(upstream, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("ok-upstream"))
		}))
	}()

	var audit strings.Builder
	px, err := StartProxy(context.Background(), Policy{
		Mode:  "allowlist",
		Allow: []string{"127.0.0.1"},
	}, "127.0.0.1:0", &audit)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = px.Close(context.Background()) }()

	proxyURL, _ := url.Parse("http://" + px.Addr())
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}

	// Allowed IP literal
	upURL := "http://" + upstream.Addr().String() + "/x"
	resp, err := client.Get(upURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "ok-upstream" {
		t.Fatalf("body %q", body)
	}

	// Denied host
	resp2, err := client.Get("http://example.com/")
	if err == nil {
		_ = resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden {
			// Transport may surface forbidden as response
			t.Fatalf("status %d", resp2.StatusCode)
		}
	}

	// Audit contains allow and deny
	sc := bufio.NewScanner(strings.NewReader(audit.String()))
	var sawAllow, sawDeny bool
	for sc.Scan() {
		var ev auditEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Action == "allow" {
			sawAllow = true
		}
		if ev.Action == "deny" {
			sawDeny = true
		}
	}
	if !sawAllow || !sawDeny {
		t.Fatalf("audit missing allow/deny: %s", audit.String())
	}
}

func TestProxyChainsUpstreamHTTP(t *testing.T) {
	var sawProxyReq atomic.Bool
	corp := httptestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawProxyReq.Store(true)
		if r.Method == http.MethodConnect {
			http.Error(w, "no CONNECT in this test", http.StatusMethodNotAllowed)
			return
		}
		// Forward-proxy style: absolute URL, corporate proxy returns body.
		_, _ = w.Write([]byte("via-corp"))
	}))
	defer corp.Close()

	origin := httptestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("origin must not be dialed directly when upstream is set")
	}))
	defer origin.Close()

	originURL, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}

	px, err := StartProxy(context.Background(), Policy{
		Mode:     "allowlist",
		Allow:    []string{originURL.Hostname()},
		Upstream: corp.URL,
	}, "127.0.0.1:0", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = px.Close(context.Background()) }()

	proxyURL, _ := url.Parse("http://" + px.Addr())
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   5 * time.Second,
	}
	resp, err := client.Get(origin.URL + "/path")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "via-corp" {
		t.Fatalf("body %q", body)
	}
	if !sawProxyReq.Load() {
		t.Fatal("corporate proxy not hit")
	}
}

func TestProxyCONNECTViaUpstream(t *testing.T) {
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = origin.Close() }()
	go func() {
		for {
			c, err := origin.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 64)
				n, _ := c.Read(buf)
				_, _ = c.Write([]byte("pong:" + string(buf[:n])))
			}(c)
		}
	}()

	var sawCONNECT atomic.Bool
	corpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = corpLn.Close() }()
	go func() {
		_ = http.Serve(corpLn, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect {
				http.Error(w, "want CONNECT", http.StatusBadRequest)
				return
			}
			sawCONNECT.Store(true)
			dest, err := net.DialTimeout("tcp", r.Host, 2*time.Second)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadGateway)
				return
			}
			hij, ok := w.(http.Hijacker)
			if !ok {
				_ = dest.Close()
				http.Error(w, "no hijack", http.StatusInternalServerError)
				return
			}
			client, _, err := hij.Hijack()
			if err != nil {
				_ = dest.Close()
				return
			}
			_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
			bidirectionalCopy(client, dest)
		}))
	}()

	destHost := origin.Addr().String()
	px, err := StartProxy(context.Background(), Policy{
		Mode:     "allowlist",
		Allow:    []string{"127.0.0.1"},
		Upstream: "http://" + corpLn.Addr().String(),
	}, "127.0.0.1:0", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = px.Close(context.Background()) }()

	// Speak CONNECT to cell-egress like an HTTPS client would.
	conn, err := net.DialTimeout("tcp", px.Addr(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_, _ = io.WriteString(conn, "CONNECT "+destHost+" HTTP/1.1\r\nHost: "+destHost+"\r\n\r\n")
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_, _ = io.WriteString(conn, "ping")
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(buf[:n]), "pong:ping") {
		t.Fatalf("got %q", buf[:n])
	}
	if !sawCONNECT.Load() {
		t.Fatal("corporate proxy did not see CONNECT")
	}
}

func TestRewriteUpstreamLoopback(t *testing.T) {
	got, need := RewriteUpstreamLoopback("http://127.0.0.1:7890")
	if !need || got != "http://host.docker.internal:7890" {
		t.Fatalf("got %q need=%v", got, need)
	}
	got, need = RewriteUpstreamLoopback("http://proxy.corp:8080")
	if need || got != "http://proxy.corp:8080" {
		t.Fatalf("got %q need=%v", got, need)
	}
}

func TestResolveUpstreamPriority(t *testing.T) {
	t.Setenv("CELL_EGRESS_UPSTREAM", "http://cell-up:1")
	t.Setenv("HTTPS_PROXY", "http://https-up:2")
	t.Setenv("HTTP_PROXY", "http://http-up:3")
	if got := ResolveUpstream(""); got != "http://cell-up:1" {
		t.Fatalf("got %q", got)
	}
	if got := ResolveUpstream("http://explicit:9"); got != "http://explicit:9" {
		t.Fatalf("explicit %q", got)
	}
	t.Setenv("CELL_EGRESS_UPSTREAM", "")
	if got := ResolveUpstream(""); got != "http://https-up:2" {
		t.Fatalf("https %q", got)
	}
}

// httptestServer is a tiny helper avoiding importing net/http/httptest name noise in older diffs.
func httptestServer(t *testing.T, h http.Handler) *httpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &httpServer{ln: ln, URL: "http://" + ln.Addr().String()}
	go func() { _ = http.Serve(ln, h) }()
	return s
}

type httpServer struct {
	ln  net.Listener
	URL string
}

func (s *httpServer) Close() { _ = s.ln.Close() }
