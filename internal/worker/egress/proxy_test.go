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
