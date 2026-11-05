// Package envtemplate loads Cell environment YAML templates (stdlib-only subset).
package envtemplate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/YanChao1999/Cell/api"
)

// CacheMount describes a host↔container cache bind.
type CacheMount struct {
	Host      string
	Container string
}

// Template is a resolved environment definition.
type Template struct {
	Name               string
	Description        string
	Image              string
	CacheMounts        []CacheMount
	Setup              []string
	Env                map[string]string
	DefaultLimits      api.Limits
	Network            string   // default | none | allowlist
	EgressAllow        []string // hostnames when network=allowlist (future proxy)
	DefaultExecTimeout string   // optional duration string, e.g. "30m", "2h"
}

// Loader reads templates from a directory (e.g. environments/go-1.23.yaml).
type Loader struct {
	Dir string
}

// New returns a Loader for dir. Empty dir means Load will error.
func New(dir string) *Loader {
	return &Loader{Dir: dir}
}

// Load reads <dir>/<name>.yaml (or .yml).
func (l *Loader) Load(name string) (*Template, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("envtemplate: empty name")
	}
	if l == nil || l.Dir == "" {
		return nil, fmt.Errorf("envtemplate: environments directory not configured")
	}
	base := filepath.Join(l.Dir, name)
	for _, ext := range []string{".yaml", ".yml"} {
		path := base + ext
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		t, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("envtemplate: parse %s: %w", path, err)
		}
		if t.Name == "" {
			t.Name = name
		}
		return t, nil
	}
	return nil, fmt.Errorf("envtemplate: %q not found under %s", name, l.Dir)
}

// Parse parses a minimal YAML subset used by environment templates.
func Parse(data []byte) (*Template, error) {
	t := &Template{Env: map[string]string{}}
	lines := splitLines(string(data))
	i := 0
	for i < len(lines) {
		line := lines[i]
		if isBlankOrComment(line) {
			i++
			continue
		}
		indent := countIndent(line)
		if indent != 0 {
			return nil, fmt.Errorf("unexpected indent at line %d: %q", i+1, line)
		}
		key, val, ok := splitKV(strings.TrimSpace(line))
		if !ok {
			return nil, fmt.Errorf("invalid line %d: %q", i+1, line)
		}
		switch key {
		case "name":
			t.Name = unquote(val)
			i++
		case "description":
			t.Description = unquote(val)
			i++
		case "image":
			t.Image = unquote(val)
			i++
		case "network":
			t.Network = unquote(val)
			i++
		case "default_exec_timeout":
			t.DefaultExecTimeout = unquote(val)
			i++
		case "egress":
			// Optional block for future allowlist proxy:
			// egress:
			//   allow:
			//     - proxy.golang.org
			i++
			for i < len(lines) {
				l := lines[i]
				if isBlankOrComment(l) {
					i++
					continue
				}
				if countIndent(l) == 0 {
					break
				}
				trim := strings.TrimSpace(l)
				ek, _, ok := splitKV(trim)
				if !ok {
					if strings.HasPrefix(trim, "-") {
						return nil, fmt.Errorf("egress: unexpected list at line %d", i+1)
					}
					return nil, fmt.Errorf("egress: bad field at line %d", i+1)
				}
				if ek != "allow" {
					return nil, fmt.Errorf("egress: unsupported field %q at line %d", ek, i+1)
				}
				baseInd := countIndent(l)
				i++
				var allow []string
				for i < len(lines) {
					al := lines[i]
					if isBlankOrComment(al) {
						i++
						continue
					}
					if countIndent(al) <= baseInd {
						break
					}
					at := strings.TrimSpace(al)
					if !strings.HasPrefix(at, "-") {
						return nil, fmt.Errorf("egress.allow: expected list item at line %d", i+1)
					}
					allow = append(allow, unquote(strings.TrimSpace(strings.TrimPrefix(at, "-"))))
					i++
				}
				t.EgressAllow = allow
			}
		case "cache_mounts":
			var mounts []CacheMount
			i++
			for i < len(lines) {
				l := lines[i]
				if isBlankOrComment(l) {
					i++
					continue
				}
				ind := countIndent(l)
				if ind == 0 {
					break
				}
				trim := strings.TrimSpace(l)
				if !strings.HasPrefix(trim, "-") {
					return nil, fmt.Errorf("cache_mounts: expected list item at line %d", i+1)
				}
				itemRest := strings.TrimSpace(strings.TrimPrefix(trim, "-"))
				m := CacheMount{}
				if itemRest != "" {
					k, v, ok := splitKV(itemRest)
					if !ok {
						return nil, fmt.Errorf("cache_mounts: bad item at line %d", i+1)
					}
					setMountField(&m, k, v)
				}
				i++
				for i < len(lines) {
					nl := lines[i]
					if isBlankOrComment(nl) {
						i++
						continue
					}
					nind := countIndent(nl)
					if nind <= ind {
						break
					}
					nk, nv, ok := splitKV(strings.TrimSpace(nl))
					if !ok {
						return nil, fmt.Errorf("cache_mounts: bad field at line %d", i+1)
					}
					setMountField(&m, nk, nv)
					i++
				}
				mounts = append(mounts, m)
			}
			t.CacheMounts = mounts
		case "setup":
			var setup []string
			i++
			for i < len(lines) {
				l := lines[i]
				if isBlankOrComment(l) {
					i++
					continue
				}
				if countIndent(l) == 0 {
					break
				}
				trim := strings.TrimSpace(l)
				if strings.HasPrefix(trim, "-") {
					setup = append(setup, unquote(strings.TrimSpace(strings.TrimPrefix(trim, "-"))))
				}
				i++
			}
			t.Setup = setup
		case "env":
			i++
			for i < len(lines) {
				l := lines[i]
				if isBlankOrComment(l) {
					i++
					continue
				}
				if countIndent(l) == 0 {
					break
				}
				k, v, ok := splitKV(strings.TrimSpace(l))
				if !ok {
					return nil, fmt.Errorf("env: bad entry at line %d", i+1)
				}
				t.Env[k] = unquote(v)
				i++
			}
		case "default_limits":
			i++
			for i < len(lines) {
				l := lines[i]
				if isBlankOrComment(l) {
					i++
					continue
				}
				if countIndent(l) == 0 {
					break
				}
				k, v, ok := splitKV(strings.TrimSpace(l))
				if !ok {
					return nil, fmt.Errorf("default_limits: bad entry at line %d", i+1)
				}
				v = unquote(v)
				switch k {
				case "cpu":
					t.DefaultLimits.CPU = v
				case "memory":
					t.DefaultLimits.Memory = v
				case "disk":
					t.DefaultLimits.Disk = v
				}
				i++
			}
		default:
			// Skip unknown top-level keys (including nested blocks).
			i++
			for i < len(lines) {
				l := lines[i]
				if isBlankOrComment(l) {
					i++
					continue
				}
				if countIndent(l) == 0 {
					break
				}
				i++
			}
		}
	}
	return t, nil
}

func setMountField(m *CacheMount, k, v string) {
	v = unquote(v)
	switch k {
	case "host":
		m.Host = v
	case "container":
		m.Container = v
	}
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func isBlankOrComment(line string) bool {
	trim := strings.TrimSpace(line)
	return trim == "" || strings.HasPrefix(trim, "#")
}

func countIndent(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
			continue
		}
		if r == '\t' {
			n += 2
			continue
		}
		break
	}
	return n
}

func splitKV(line string) (key, val string, ok bool) {
	// Strip trailing inline comment when not inside quotes (best-effort).
	line = stripInlineComment(line)
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:i])
	val = strings.TrimSpace(line[i+1:])
	if key == "" {
		return "", "", false
	}
	return key, val, true
}

func stripInlineComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(line[:i])
			}
		}
	}
	return line
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
