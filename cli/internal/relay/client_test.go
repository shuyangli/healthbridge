package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRelay is a tiny in-memory relay used by client tests. It implements
// just enough of the protocol to verify the client's request shape and
// response decoding; it intentionally does not exercise the long-poll waker
// (the real relay's mailbox tests cover that). End-to-end behaviour is
// covered by the scenario test.
type fakeRelay struct {
	mu      sync.Mutex
	jobs    []JobBlob
	results map[string][]ResultBlob
	nextSeq int64
	revoked bool
	server  *httptest.Server
	t       *testing.T
}

func newFakeRelay(t *testing.T) *fakeRelay {
	t.Helper()
	r := &fakeRelay{
		results: make(map[string][]ResultBlob),
		t:       t,
	}
	r.server = httptest.NewServer(http.HandlerFunc(r.handle))
	t.Cleanup(r.server.Close)
	return r
}

func (r *fakeRelay) handle(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	if q.Get("pair") == "" {
		http.Error(w, `{"code":"missing_pair","message":"x"}`, 400)
		return
	}
	switch req.URL.Path + "|" + req.Method {
	case "/v1/jobs|POST":
		r.handlePostJob(w, req)
	case "/v1/jobs|GET":
		r.handleGetJobs(w, req, q)
	case "/v1/results|POST":
		r.handlePostResult(w, req)
	case "/v1/results|GET":
		r.handleGetResults(w, q)
	case "/v1/pair|DELETE":
		r.mu.Lock()
		r.revoked = true
		r.jobs = nil
		r.results = make(map[string][]ResultBlob)
		r.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true})
	default:
		http.Error(w, `{"code":"not_found","message":"x"}`, 404)
	}
}

func (r *fakeRelay) handlePostJob(w http.ResponseWriter, req *http.Request) {
	var body struct {
		JobID string `json:"job_id"`
		Blob  string `json:"blob"`
	}
	if err := decodeJSON(req, &body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if body.JobID == "" || body.Blob == "" {
		writeJSON(w, 400, map[string]any{"code": "missing", "message": "x"})
		return
	}
	r.mu.Lock()
	r.nextSeq++
	jb := JobBlob{
		Seq:        r.nextSeq,
		JobID:      body.JobID,
		Blob:       body.Blob,
		EnqueuedAt: 1000,
		ExpiresAt:  9000,
	}
	r.jobs = append(r.jobs, jb)
	r.mu.Unlock()
	writeJSON(w, 201, map[string]any{
		"job_id":      jb.JobID,
		"seq":         jb.Seq,
		"enqueued_at": jb.EnqueuedAt,
		"expires_at":  jb.ExpiresAt,
	})
}

func (r *fakeRelay) handleGetJobs(w http.ResponseWriter, _ *http.Request, q url.Values) {
	since := parseInt64(q.Get("since"))
	r.mu.Lock()
	defer r.mu.Unlock()
	var page []JobBlob
	for _, j := range r.jobs {
		if j.Seq > since {
			page = append(page, j)
		}
	}
	cursor := since
	if len(page) > 0 {
		cursor = page[len(page)-1].Seq
	}
	writeJSON(w, 200, JobsPage{Jobs: page, NextCursor: cursor})
}

func (r *fakeRelay) handlePostResult(w http.ResponseWriter, req *http.Request) {
	var body struct {
		JobID     string `json:"job_id"`
		PageIndex int    `json:"page_index"`
		Blob      string `json:"blob"`
	}
	if err := decodeJSON(req, &body); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	r.mu.Lock()
	r.results[body.JobID] = append(r.results[body.JobID], ResultBlob{
		JobID:     body.JobID,
		PageIndex: body.PageIndex,
		Blob:      body.Blob,
		PostedAt:  1000,
		ExpiresAt: 5000,
	})
	r.mu.Unlock()
	writeJSON(w, 201, map[string]any{
		"job_id":      body.JobID,
		"page_index":  body.PageIndex,
		"posted_at":   1000,
		"expires_at":  5000,
	})
}

func (r *fakeRelay) handleGetResults(w http.ResponseWriter, q url.Values) {
	id := q.Get("job_id")
	r.mu.Lock()
	defer r.mu.Unlock()
	writeJSON(w, 200, ResultsResponse{Results: r.results[id]})
}

func decodeJSON(req *http.Request, out any) error {
	defer req.Body.Close()
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int64(c-'0')
	}
	return n
}

// ---- tests --------------------------------------------------------------

func TestEnqueueJobAndPollJobs(t *testing.T) {
	r := newFakeRelay(t)
	c := New(r.server.URL, "01J9ZX0PAIR000000000000001")
	ctx := context.Background()

	if _, err := c.EnqueueJob(ctx, "job-1", "blob-1"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := c.EnqueueJob(ctx, "job-2", "blob-2"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	page, err := c.PollJobs(ctx, 0, 0)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(page.Jobs) != 2 {
		t.Fatalf("got %d jobs, want 2", len(page.Jobs))
	}
	if page.Jobs[0].JobID != "job-1" || page.Jobs[1].JobID != "job-2" {
		t.Errorf("unexpected job order: %+v", page.Jobs)
	}
	if page.NextCursor != 2 {
		t.Errorf("next_cursor = %d, want 2", page.NextCursor)
	}

	// Second poll with the new cursor returns nothing.
	page2, err := c.PollJobs(ctx, page.NextCursor, 0)
	if err != nil {
		t.Fatalf("poll2: %v", err)
	}
	if len(page2.Jobs) != 0 {
		t.Errorf("expected empty page, got %d", len(page2.Jobs))
	}
}

func TestPostResultAndPollResults(t *testing.T) {
	r := newFakeRelay(t)
	c := New(r.server.URL, "01J9ZX0PAIR000000000000001")
	ctx := context.Background()

	if _, err := c.PostResult(ctx, "job-x", 0, "result-blob"); err != nil {
		t.Fatalf("post result: %v", err)
	}

	res, err := c.PollResults(ctx, "job-x", 0)
	if err != nil {
		t.Fatalf("poll results: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].Blob != "result-blob" {
		t.Errorf("unexpected results: %+v", res)
	}
}

func TestRevokePair(t *testing.T) {
	r := newFakeRelay(t)
	c := New(r.server.URL, "01J9ZX0PAIR000000000000001")
	ctx := context.Background()

	if _, err := c.EnqueueJob(ctx, "job-1", "blob"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if err := c.RevokePair(ctx); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.revoked || len(r.jobs) != 0 {
		t.Errorf("relay state not cleared: revoked=%v jobs=%d", r.revoked, len(r.jobs))
	}
}

func TestErrorResponseDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 429, map[string]any{
			"code":    "mailbox_full",
			"message": "too many",
		})
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "01J9ZX0PAIR000000000000001")
	_, err := c.EnqueueJob(context.Background(), "j", "b")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsRelayCode(err, "mailbox_full") {
		t.Errorf("expected mailbox_full code, got %v", err)
	}
	var re *Error
	if !errors.As(err, &re) {
		t.Fatalf("expected *relay.Error, got %T", err)
	}
	if re.HTTPStatus != 429 {
		t.Errorf("HTTPStatus = %d, want 429", re.HTTPStatus)
	}
}

func TestNonJSONErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
		_, _ = io.WriteString(w, "upstream offline")
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "01J9ZX0PAIR000000000000001")
	_, err := c.PollJobs(context.Background(), 0, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "upstream offline") {
		t.Errorf("expected wrapped body, got %v", err)
	}
}

func TestContextCancelStopsLongPoll(t *testing.T) {
	// Server that hangs forever — context should cancel the request.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		<-req.Context().Done()
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, "01J9ZX0PAIR000000000000001")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.PollJobs(ctx, 0, 25_000)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
