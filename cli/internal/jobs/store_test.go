package jobs

import (
	"database/sql"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newPendingRecord(id string) *JobRecord {
	return &JobRecord{
		ID:          id,
		PairID:      "01J9ZX0PAIR000000000000001",
		Kind:        "read",
		PayloadJSON: []byte(`{"type":"step_count"}`),
	}
}

func TestEnqueueAndGet(t *testing.T) {
	s := newStore(t)
	rec := newPendingRecord("job-1")
	if err := s.Enqueue(rec); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "job-1" || got.Status != StatusPending || got.Kind != "read" {
		t.Errorf("unexpected record: %+v", got)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be auto-populated")
	}
}

func TestEnqueueRequiresFields(t *testing.T) {
	s := newStore(t)
	if err := s.Enqueue(&JobRecord{}); err == nil {
		t.Error("expected error for empty record")
	}
}

func TestGetMissingReturnsErrNoRows(t *testing.T) {
	s := newStore(t)
	_, err := s.Get("nope")
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestListByStatus(t *testing.T) {
	s := newStore(t)
	if err := s.Enqueue(newPendingRecord("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(newPendingRecord("b")); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDone("a", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	pending, err := s.List(ListFilter{Status: StatusPending})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != "b" {
		t.Errorf("pending = %+v, want only b", pending)
	}
	done, err := s.List(ListFilter{Status: StatusDone})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].ID != "a" {
		t.Errorf("done = %+v, want only a", done)
	}
}

func TestListByPair(t *testing.T) {
	s := newStore(t)
	a := newPendingRecord("a")
	a.PairID = "pair-1"
	b := newPendingRecord("b")
	b.PairID = "pair-2"
	if err := s.Enqueue(a); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(b); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(ListFilter{PairID: "pair-2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("got %+v", got)
	}
}

func TestMarkDoneStoresResult(t *testing.T) {
	s := newStore(t)
	if err := s.Enqueue(newPendingRecord("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDone("a", []byte(`{"uuid":"x"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("a")
	if got.Status != StatusDone {
		t.Errorf("status = %q, want done", got.Status)
	}
	if string(got.ResultJSON) != `{"uuid":"x"}` {
		t.Errorf("result_json = %q", got.ResultJSON)
	}
	if got.CompletedAt.IsZero() {
		t.Error("CompletedAt should be set")
	}
}

func TestMarkFailedCarriesError(t *testing.T) {
	s := newStore(t)
	if err := s.Enqueue(newPendingRecord("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkFailed("a", "scope_denied", "no auth"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("a")
	if got.Status != StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.ErrorCode != "scope_denied" || got.ErrorMessage != "no auth" {
		t.Errorf("err = %q/%q", got.ErrorCode, got.ErrorMessage)
	}
}

func TestCancelOnlyPending(t *testing.T) {
	s := newStore(t)
	if err := s.Enqueue(newPendingRecord("a")); err != nil {
		t.Fatal(err)
	}
	if err := s.Cancel("a"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("a")
	if got.Status != StatusCanceled {
		t.Errorf("status = %q, want canceled", got.Status)
	}
	// Cancelling again should fail.
	if err := s.Cancel("a"); err == nil {
		t.Error("expected error cancelling already-canceled job")
	}
}

func TestExpireOverdue(t *testing.T) {
	s := newStore(t)
	rec := newPendingRecord("a")
	rec.Deadline = time.Now().Add(-1 * time.Minute)
	if err := s.Enqueue(rec); err != nil {
		t.Fatal(err)
	}
	n, err := s.ExpireOverdue(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("expired = %d, want 1", n)
	}
	got, _ := s.Get("a")
	if got.Status != StatusExpired {
		t.Errorf("status = %q, want expired", got.Status)
	}
}

func TestPruneRemovesTerminalRows(t *testing.T) {
	s := newStore(t)
	old := newPendingRecord("old")
	old.CreatedAt = time.Now().Add(-48 * time.Hour)
	if err := s.Enqueue(old); err != nil {
		t.Fatal(err)
	}
	_ = s.MarkDone("old", []byte(`{}`))
	if err := s.Enqueue(newPendingRecord("fresh")); err != nil {
		t.Fatal(err)
	}
	n, err := s.Prune(time.Now().Add(-1 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned = %d, want 1", n)
	}
	if _, err := s.Get("old"); err != sql.ErrNoRows {
		t.Errorf("expected old to be gone, got %v", err)
	}
	// Fresh pending should not be touched even though it's "older than"
	// when computed against created_at, because it's still pending.
	fresh, err := s.Get("fresh")
	if err != nil || fresh.Status != StatusPending {
		t.Errorf("fresh got %+v %v", fresh, err)
	}
}
