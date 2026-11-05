// Package meta stores durable Cell metadata (sticky worker placement).
package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Record is persisted Cell metadata.
type Record struct {
	ID          string    `json:"id"`
	WorkerID    string    `json:"worker_id"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Repo        string    `json:"repo,omitempty"`
	Ref         string    `json:"ref,omitempty"`
}

// Store is an in-memory meta store with optional JSON persistence.
type Store struct {
	mu   sync.RWMutex
	byID map[string]*Record
	dir  string // empty = memory only
	path string
}

// New creates a Store. If stateDir is non-empty, records are persisted to
// stateDir/cells.json under ~/.cell/state/ typically.
func New(stateDir string) (*Store, error) {
	s := &Store{
		byID: make(map[string]*Record),
		dir:  stateDir,
	}
	if stateDir != "" {
		if err := os.MkdirAll(stateDir, 0o755); err != nil {
			return nil, err
		}
		s.path = filepath.Join(stateDir, "cells.json")
		if err := s.load(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var records []*Record
	if err := json.Unmarshal(data, &records); err != nil {
		return fmt.Errorf("meta: load %s: %w", s.path, err)
	}
	for _, r := range records {
		if r != nil && r.ID != "" {
			s.byID[r.ID] = r
		}
	}
	return nil
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	records := make([]*Record, 0, len(s.byID))
	for _, r := range s.byID {
		records = append(records, r)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Put inserts or updates a record.
func (s *Store) Put(r *Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *r
	s.byID[r.ID] = &cp
	return s.persistLocked()
}

// Get returns a record by cell id.
func (s *Store) Get(id string) (*Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.byID[id]
	if !ok {
		return nil, false
	}
	cp := *r
	return &cp, true
}

// Delete removes a record.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
	return s.persistLocked()
}

// List returns all records.
func (s *Store) List() []*Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Record, 0, len(s.byID))
	for _, r := range s.byID {
		cp := *r
		out = append(out, &cp)
	}
	return out
}

// WorkerID returns the sticky worker for a cell, if known.
func (s *Store) WorkerID(cellID string) (string, bool) {
	r, ok := s.Get(cellID)
	if !ok {
		return "", false
	}
	return r.WorkerID, true
}

// Expired returns records with ExpiresAt in the past (non-zero).
func (s *Store) Expired(now time.Time) []*Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*Record
	for _, r := range s.byID {
		if !r.ExpiresAt.IsZero() && !r.ExpiresAt.After(now) {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out
}
