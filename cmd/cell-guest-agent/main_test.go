package main

import "testing"

func TestNameserverFromCmdline(t *testing.T) {
	got := nameserverFromCmdline("console=ttyS0 cell.nameserver=172.26.9.1 reboot=k")
	if got != "172.26.9.1" {
		t.Fatalf("got %q", got)
	}
	if nameserverFromCmdline("console=ttyS0") != "" {
		t.Fatal("expected empty")
	}
	if nameserverFromCmdline("cell.nameserver=not-an-ip") != "" {
		t.Fatal("expected reject")
	}
}
