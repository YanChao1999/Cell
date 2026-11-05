// Command cell-egress is the allowlist HTTP CONNECT/forward proxy used as a
// Docker sidecar for network: allowlist Cells.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/YanChao1999/Cell/internal/worker/egress"
)

func main() {
	listen := flag.String("listen", ":3128", "listen address")
	auditPath := flag.String("audit", "", "audit log file (JSON lines); empty = stdout")
	var allows allowFlags
	flag.Var(&allows, "allow", "allowed hostname (repeatable)")
	flag.Parse()

	if len(allows) == 0 {
		fmt.Fprintln(os.Stderr, "cell-egress: at least one -allow host is required")
		os.Exit(2)
	}

	var auditFile *os.File
	var err error
	if *auditPath == "" {
		auditFile = os.Stdout
	} else {
		auditFile, err = egress.OpenAuditFile(*auditPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cell-egress: audit: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = auditFile.Close() }()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := egress.StartProxy(ctx, egress.Policy{Mode: "allowlist", Allow: allows}, *listen, auditFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cell-egress: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "cell-egress listening on %s allow=%s\n", srv.Addr(), strings.Join(allows, ","))
	<-ctx.Done()
	_ = srv.Close(context.Background())
}

type allowFlags []string

func (a *allowFlags) String() string { return strings.Join(*a, ",") }
func (a *allowFlags) Set(v string) error {
	*a = append(*a, v)
	return nil
}
