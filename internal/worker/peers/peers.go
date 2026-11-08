// Package peers discovers warm cache donors via the control-plane worker list.
package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/YanChao1999/Cell/internal/control/registry"
	"github.com/YanChao1999/Cell/internal/worker/auth"
)

// Finder looks up remote worker endpoints that advertise a warm cache key.
type Finder struct {
	ControlURL string
	Token      string
	Client     *http.Client
}

// EndpointForCache returns an http peer endpoint that has key, excluding selfID.
func (f *Finder) EndpointForCache(ctx context.Context, key, selfID string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || f == nil || strings.TrimSpace(f.ControlURL) == "" {
		return "", nil
	}
	base := strings.TrimRight(f.ControlURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/workers", nil)
	if err != nil {
		return "", err
	}
	auth.SetBearer(req.Header, f.Token)
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("peers: list workers: %s", resp.Status)
	}
	var workers []registry.Info
	if err := json.NewDecoder(resp.Body).Decode(&workers); err != nil {
		return "", err
	}
	for _, w := range workers {
		if w.ID == selfID {
			continue
		}
		ep := strings.TrimSpace(w.Endpoint)
		if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
			continue
		}
		for _, k := range w.CacheKeys {
			if k == key {
				return strings.TrimRight(ep, "/"), nil
			}
		}
	}
	return "", nil
}
