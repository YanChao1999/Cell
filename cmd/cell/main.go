// Command cell is a thin ops CLI for the Cell agent API.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "health":
		err = doHealth()
	case "list":
		err = doList()
	case "run":
		err = doRun(os.Args[2:])
	case "destroy":
		if len(os.Args) < 3 {
			err = fmt.Errorf("usage: cell destroy <id>")
		} else {
			err = doDestroy(os.Args[2])
		}
	case "version", "-version", "--version":
		fmt.Println("cell 0.3.0")
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `cell — thin ops CLI for the Cell agent API

Usage:
  cell health
  cell list
  cell run [--env NAME] [--repo URL] [--ref REF] <command> [command...]
  cell destroy <id>
  cell version

Socket: CELL_SOCKET or ~/.cell/celld.sock
Primary agent client is the Python SDK (sdk/python).
`)
}

func socketPath() string {
	if p := os.Getenv("CELL_SOCKET"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", "celld.sock")
	}
	return filepath.Join(home, ".cell", "celld.sock")
}

func unixClient() *http.Client {
	path := socketPath()
	return &http.Client{
		Timeout: 120 * time.Second,
		Transport: &http.Transport{
			Dial: func(_, _ string) (net.Conn, error) {
				return net.Dial("unix", path)
			},
		},
	}
}

func prettyJSON(body []byte) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		fmt.Println(string(body))
		return
	}
	fmt.Println(buf.String())
}

func getAndPrintJSON(url string) error {
	resp, err := unixClient().Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	prettyJSON(body)
	return nil
}

func doHealth() error {
	return getAndPrintJSON("http://localhost/health")
}

func doList() error {
	return getAndPrintJSON("http://localhost/v1/cells")
}

func doDestroy(id string) error {
	req, err := http.NewRequest(http.MethodDelete, "http://localhost/v1/cells/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := unixClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("destroy: %s: %s", resp.Status, body)
	}
	fmt.Println("destroyed", id)
	return nil
}

func doRun(args []string) error {
	env := "go-1.23"
	repo := ""
	ref := "main"
	var cmds []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--env":
			i++
			if i >= len(args) {
				return fmt.Errorf("--env needs a value")
			}
			env = args[i]
		case "--repo":
			i++
			if i >= len(args) {
				return fmt.Errorf("--repo needs a value")
			}
			repo = args[i]
		case "--ref":
			i++
			if i >= len(args) {
				return fmt.Errorf("--ref needs a value")
			}
			ref = args[i]
		default:
			cmds = append(cmds, args[i])
		}
	}
	if len(cmds) == 0 {
		return fmt.Errorf("usage: cell run [--env NAME] [--repo URL] [--ref REF] <command>")
	}
	payload := map[string]any{
		"environment":  env,
		"repo":         repo,
		"ref":          ref,
		"ttl":          "30m",
		"prefer_local": true,
		"commands":     cmds,
	}
	raw, _ := json.Marshal(payload)
	resp, err := unixClient().Post("http://localhost/v1/run", "application/json", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("run: %s: %s", resp.Status, body)
	}
	prettyJSON(body)
	// Non-zero if any command failed
	var parsed struct {
		Results []struct {
			ExitCode int `json:"exit_code"`
		} `json:"results"`
	}
	_ = json.Unmarshal(body, &parsed)
	for _, r := range parsed.Results {
		if r.ExitCode != 0 {
			return fmt.Errorf("command failed with exit %d", r.ExitCode)
		}
	}
	return nil
}
