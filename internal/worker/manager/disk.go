package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// checkWorkspaceDisk enforces a portable disk cap by measuring host workspace
// usage (du). Works without Docker storage-opt / xfs pquota. Empty limit = no-op.
func checkWorkspaceDisk(workspace, limit string) error {
	limit = strings.TrimSpace(limit)
	if limit == "" || workspace == "" {
		return nil
	}
	max, err := parseDiskBytes(limit)
	if err != nil {
		return fmt.Errorf("manager: disk limit %q: %w", limit, err)
	}
	used, err := dirSize(workspace)
	if err != nil {
		return fmt.Errorf("manager: measure workspace disk: %w", err)
	}
	if used > max {
		return fmt.Errorf("manager: workspace disk %s exceeds limit %s (%d > %d bytes)",
			workspace, limit, used, max)
	}
	return nil
}

func parseDiskBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	multipliers := []struct {
		suffix string
		mult   int64
	}{
		{"Gi", 1 << 30},
		{"Mi", 1 << 20},
		{"Ki", 1 << 10},
		{"G", 1_000_000_000},
		{"M", 1_000_000},
		{"K", 1_000},
		{"g", 1 << 30},
		{"m", 1 << 20},
		{"k", 1 << 10},
	}
	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			n := strings.TrimSpace(strings.TrimSuffix(s, m.suffix))
			f, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return 0, err
			}
			return int64(f * float64(m.mult)), nil
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
