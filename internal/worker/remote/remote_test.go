package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/YanChao1999/Cell/api"
	"github.com/YanChao1999/Cell/internal/workerapi"
)

func TestRemoteCreateExecDestroy(t *testing.T) {
	const token = "test-token"
	mux := http.NewServeMux()
	cells := map[string]*workerapi.Cell{}
	mux.HandleFunc("POST /worker/v1/cells", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c := &workerapi.Cell{ID: "c1", Environment: "go-1.23", Status: "ready"}
		cells[c.ID] = c
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(c)
	})
	mux.HandleFunc("POST /worker/v1/cells/{id}/exec", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.ExecResult{ExitCode: 0, Stdout: "hi\n"})
	})
	mux.HandleFunc("DELETE /worker/v1/cells/{id}", func(w http.ResponseWriter, r *http.Request) {
		delete(cells, r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rw, err := New("w1", srv.URL, Options{Token: token})
	if err != nil {
		t.Fatal(err)
	}
	rw.UpdateCapacity(4, 0, []string{"go-mod"})
	if rw.Capacity().Free() != 4 || !rw.CacheIndex().Has("go-mod") {
		t.Fatalf("capacity/index: %+v", rw.Capacity())
	}
	cell, err := rw.CreateCell(context.Background(), api.CreateCellRequest{Environment: "go-1.23"})
	if err != nil || cell.ID != "c1" {
		t.Fatalf("%+v %v", cell, err)
	}
	res, err := rw.Exec(context.Background(), "c1", api.ExecRequest{Command: "echo"})
	if err != nil || res.Stdout != "hi\n" {
		t.Fatalf("%+v %v", res, err)
	}
	if err := rw.DestroyCell(context.Background(), "c1"); err != nil {
		t.Fatal(err)
	}
}
