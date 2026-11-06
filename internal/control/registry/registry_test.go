package registry

import (
	"testing"
	"time"
)

func TestRegisterHeartbeatExpire(t *testing.T) {
	r := New(50 * time.Millisecond)
	info, err := r.Register(RegisterRequest{ID: "w1", MaxCells: 4})
	if err != nil || info.ID != "w1" {
		t.Fatalf("%+v %v", info, err)
	}
	if _, err := r.Heartbeat("w1", HeartbeatRequest{UsedCells: 1}); err != nil {
		t.Fatal(err)
	}
	if len(r.List()) != 1 {
		t.Fatal("expected live worker")
	}
	time.Sleep(80 * time.Millisecond)
	if len(r.List()) != 0 {
		t.Fatal("expected expiry")
	}
	if _, err := r.Heartbeat("w1", HeartbeatRequest{}); err == nil {
		t.Fatal("heartbeat after expiry should fail")
	}
}
