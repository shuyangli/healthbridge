// Package fakerelay is a test-only in-memory implementation of the
// healthbridge relay's HTTP surface. It exists so Go-side tests (CLI
// scenario tests, future jobs/sync tests) can drive the protocol without
// needing the TypeScript Worker, miniflare, or any external process.
//
// It is API-compatible with the real relay for the routes implemented in
// M1 (POST/GET /v1/jobs, POST/GET /v1/results, DELETE /v1/pair, GET
// /v1/health) and supports waker-based long-polling so end-to-end tests
// can race a "drainer" goroutine against a "client" goroutine the same
// way the production system races the iOS app against the CLI.
package fakerelay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

// Server is a fake relay backed by httptest.NewServer.
type Server struct {
	srv *httptest.Server

	mu           sync.Mutex
	jobs         []storedJob
	results      map[string][]storedResult
	nextSeq      int64
	jobWaiters   []chan struct{}
	resultWaits  map[string][]chan struct{}
	maxLongPoll  time.Duration
}

type storedJob struct {
	Seq        int64  `json:"seq"`
	JobID      string `json:"job_id"`
	Blob       string `json:"blob"`
	EnqueuedAt int64  `json:"enqueued_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

type storedResult struct {
	JobID     string `json:"job_id"`
	PageIndex int    `json:"page_index"`
	Blob      string `json:"blob"`
	PostedAt  int64  `json:"posted_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// New starts a fake relay listening on a local random port. Caller must
// call Close (or use t.Cleanup).
func New() *Server {
	s := &Server{
		results:     make(map[string][]storedResult),
		resultWaits: make(map[string][]chan struct{}),
		maxLongPoll: 5 * time.Second,
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	return s
}

// URL returns the base URL the relay is listening on.
func (s *Server) URL() string { return s.srv.URL }

// Close shuts the server down.
func (s *Server) Close() { s.srv.Close() }

// SetMaxLongPoll caps how long the relay will block on a long-poll. Tests
// usually set this very low so an unmocked client doesn't hang the suite.
func (s *Server) SetMaxLongPoll(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxLongPoll = d
}

// PendingJobCount is a test convenience.
func (s *Server) PendingJobCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jobs)
}

// ---- HTTP routing -------------------------------------------------------

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("pair") == "" && r.URL.Path != "/v1/health" {
		writeErr(w, 400, "missing_pair", "pair query parameter required")
		return
	}
	switch r.URL.Path + "|" + r.Method {
	case "/v1/health|GET":
		writeJSON(w, 200, map[string]any{"ok": true})
	case "/v1/jobs|POST":
		s.enqueueJob(w, r)
	case "/v1/jobs|GET":
		s.pollJobs(w, r)
	case "/v1/results|POST":
		s.postResult(w, r)
	case "/v1/results|GET":
		s.pollResults(w, r)
	case "/v1/pair|DELETE":
		s.revoke(w)
	default:
		writeErr(w, 404, "not_found", r.Method+" "+r.URL.Path)
	}
}

func (s *Server) enqueueJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		JobID string `json:"job_id"`
		Blob  string `json:"blob"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error())
		return
	}
	if body.JobID == "" {
		writeErr(w, 400, "missing_job_id", "job_id required")
		return
	}
	if body.Blob == "" {
		writeErr(w, 400, "missing_blob", "blob required")
		return
	}
	s.mu.Lock()
	s.nextSeq++
	now := time.Now().UnixMilli()
	j := storedJob{
		Seq:        s.nextSeq,
		JobID:      body.JobID,
		Blob:       body.Blob,
		EnqueuedAt: now,
		ExpiresAt:  now + (7 * 24 * 60 * 60 * 1000),
	}
	s.jobs = append(s.jobs, j)
	wakers := s.jobWaiters
	s.jobWaiters = nil
	s.mu.Unlock()
	for _, c := range wakers {
		close(c)
	}
	writeJSON(w, 201, map[string]any{
		"job_id":      j.JobID,
		"seq":         j.Seq,
		"enqueued_at": j.EnqueuedAt,
		"expires_at":  j.ExpiresAt,
	})
}

func (s *Server) pollJobs(w http.ResponseWriter, r *http.Request) {
	since := parseInt64(r.URL.Query().Get("since"))
	waitMs := parseInt64(r.URL.Query().Get("wait_ms"))

	s.mu.Lock()
	page := jobsAfter(s.jobs, since)
	if len(page) > 0 || waitMs == 0 {
		cursor := since
		if len(page) > 0 {
			cursor = page[len(page)-1].Seq
		}
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"jobs": page, "next_cursor": cursor})
		return
	}
	wake := make(chan struct{})
	s.jobWaiters = append(s.jobWaiters, wake)
	maxWait := s.maxLongPoll
	s.mu.Unlock()

	wait := time.Duration(waitMs) * time.Millisecond
	if wait > maxWait {
		wait = maxWait
	}
	select {
	case <-wake:
	case <-time.After(wait):
	case <-r.Context().Done():
		return
	}
	s.mu.Lock()
	page = jobsAfter(s.jobs, since)
	cursor := since
	if len(page) > 0 {
		cursor = page[len(page)-1].Seq
	}
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"jobs": page, "next_cursor": cursor})
}

func (s *Server) postResult(w http.ResponseWriter, r *http.Request) {
	var body struct {
		JobID     string `json:"job_id"`
		PageIndex int    `json:"page_index"`
		Blob      string `json:"blob"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid_json", err.Error())
		return
	}
	if body.JobID == "" {
		writeErr(w, 400, "missing_job_id", "job_id required")
		return
	}
	now := time.Now().UnixMilli()
	r2 := storedResult{
		JobID:     body.JobID,
		PageIndex: body.PageIndex,
		Blob:      body.Blob,
		PostedAt:  now,
		ExpiresAt: now + (24 * 60 * 60 * 1000),
	}
	s.mu.Lock()
	s.results[body.JobID] = append(s.results[body.JobID], r2)
	wakers := s.resultWaits[body.JobID]
	delete(s.resultWaits, body.JobID)
	s.mu.Unlock()
	for _, c := range wakers {
		close(c)
	}
	writeJSON(w, 201, map[string]any{
		"job_id":     r2.JobID,
		"page_index": r2.PageIndex,
		"posted_at":  r2.PostedAt,
		"expires_at": r2.ExpiresAt,
	})
}

func (s *Server) pollResults(w http.ResponseWriter, r *http.Request) {
	jobID := r.URL.Query().Get("job_id")
	if jobID == "" {
		writeErr(w, 400, "missing_job_id", "job_id required")
		return
	}
	waitMs := parseInt64(r.URL.Query().Get("wait_ms"))

	s.mu.Lock()
	if rs, ok := s.results[jobID]; ok && len(rs) > 0 {
		out := append([]storedResult(nil), rs...)
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"results": out})
		return
	}
	if waitMs == 0 {
		s.mu.Unlock()
		writeJSON(w, 200, map[string]any{"results": []storedResult{}})
		return
	}
	wake := make(chan struct{})
	s.resultWaits[jobID] = append(s.resultWaits[jobID], wake)
	maxWait := s.maxLongPoll
	s.mu.Unlock()

	wait := time.Duration(waitMs) * time.Millisecond
	if wait > maxWait {
		wait = maxWait
	}
	select {
	case <-wake:
	case <-time.After(wait):
	case <-r.Context().Done():
		return
	}
	s.mu.Lock()
	out := append([]storedResult(nil), s.results[jobID]...)
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"results": out})
}

func (s *Server) revoke(w http.ResponseWriter) {
	s.mu.Lock()
	s.jobs = nil
	s.results = make(map[string][]storedResult)
	s.nextSeq = 0
	wakers := s.jobWaiters
	s.jobWaiters = nil
	rwakers := s.resultWaits
	s.resultWaits = make(map[string][]chan struct{})
	s.mu.Unlock()
	for _, c := range wakers {
		close(c)
	}
	for _, list := range rwakers {
		for _, c := range list {
			close(c)
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---- helpers ------------------------------------------------------------

func jobsAfter(jobs []storedJob, since int64) []storedJob {
	out := jobs[:0:0]
	for _, j := range jobs {
		if j.Seq > since {
			out = append(out, j)
		}
	}
	return out
}

func readJSON(r *http.Request, out any) error {
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"code": code, "message": msg})
}

func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	var n int64
	_, err := fmt.Sscan(s, &n)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
