// Package relay is the CLI's HTTP client for the healthbridge relay.
//
// It speaks the protocol described in relay/README.md: enqueue jobs as
// opaque blobs (in M1 these are plaintext base64; in M2+ they are
// XChaCha20-Poly1305 ciphertext) and long-poll for result blobs.
//
// Everything in this package is purely about transport — there is no
// awareness of the job payload schema or of crypto. Higher layers (the jobs
// package and the subcommands) wrap raw blobs around typed data.
package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// DefaultLongPollMs is how long the relay's long-poll endpoints will block
// before returning empty. The relay caps this internally; we send the same
// value as a hint.
const DefaultLongPollMs = 25_000

// Client is a typed wrapper around http.Client targeted at one relay base
// URL and one pair_id.
type Client struct {
	BaseURL string
	PairID  string
	// AuthToken is the per-pair Bearer credential the relay issued at
	// pairing time. Empty during the pairing flow itself; required for
	// every other endpoint after pairing has completed.
	AuthToken string
	HTTP      *http.Client
}

// New constructs a Client. baseURL must include the scheme but no trailing
// slash, e.g. "https://healthbridge-relay.example.workers.dev". The CLI is
// always pinned to one pair at a time. Use WithAuthToken() to attach the
// token after pairing.
func New(baseURL, pairID string) *Client {
	return &Client{
		BaseURL: baseURL,
		PairID:  pairID,
		HTTP: &http.Client{
			// Long-poll calls override this per-request via context.
			Timeout: 60 * time.Second,
		},
	}
}

// WithAuthToken returns a copy of this client with the bearer token set.
// Use this immediately after persisting a pair record, so the rest of the
// CLI's calls go through authenticated.
func (c *Client) WithAuthToken(token string) *Client {
	cp := *c
	cp.AuthToken = token
	return &cp
}

// Error is the structured error returned by the relay.
type Error struct {
	HTTPStatus int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("relay: %s: %s (status %d)", e.Code, e.Message, e.HTTPStatus)
}

// IsRelayCode reports whether err is a relay Error with the given code.
func IsRelayCode(err error, code string) bool {
	var re *Error
	if errors.As(err, &re) {
		return re.Code == code
	}
	return false
}

// EnqueuedJob is what the relay echoes back from POST /v1/jobs.
type EnqueuedJob struct {
	JobID      string `json:"job_id"`
	Seq        int64  `json:"seq"`
	EnqueuedAt int64  `json:"enqueued_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

// EnqueueJob posts an opaque job blob and returns the relay's ack.
func (c *Client) EnqueueJob(ctx context.Context, jobID, blob string) (*EnqueuedJob, error) {
	body := map[string]any{"job_id": jobID, "blob": blob}
	var out EnqueuedJob
	if err := c.do(ctx, "POST", "/v1/jobs", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// JobBlob is one entry in the relay's GET /v1/jobs response.
type JobBlob struct {
	Seq        int64  `json:"seq"`
	JobID      string `json:"job_id"`
	Blob       string `json:"blob"`
	EnqueuedAt int64  `json:"enqueued_at"`
	ExpiresAt  int64  `json:"expires_at"`
}

// JobsPage is the response from GET /v1/jobs.
type JobsPage struct {
	Jobs       []JobBlob `json:"jobs"`
	NextCursor int64     `json:"next_cursor"`
}

// PollJobs long-polls for jobs whose seq > since. waitMs is sent as
// wait_ms; the relay will return early if a job arrives sooner.
func (c *Client) PollJobs(ctx context.Context, since int64, waitMs int) (*JobsPage, error) {
	q := url.Values{}
	q.Set("since", fmt.Sprintf("%d", since))
	q.Set("wait_ms", fmt.Sprintf("%d", waitMs))
	var out JobsPage
	if err := c.do(ctx, "GET", "/v1/jobs", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PostedResult is what the relay echoes back from POST /v1/results.
type PostedResult struct {
	JobID     string `json:"job_id"`
	PageIndex int    `json:"page_index"`
	PostedAt  int64  `json:"posted_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// PostResult posts one result page for a previously-enqueued job.
func (c *Client) PostResult(ctx context.Context, jobID string, pageIndex int, blob string) (*PostedResult, error) {
	body := map[string]any{
		"job_id":     jobID,
		"page_index": pageIndex,
		"blob":       blob,
	}
	var out PostedResult
	if err := c.do(ctx, "POST", "/v1/results", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResultBlob is one entry in the relay's GET /v1/results response.
type ResultBlob struct {
	JobID     string `json:"job_id"`
	PageIndex int    `json:"page_index"`
	Blob      string `json:"blob"`
	PostedAt  int64  `json:"posted_at"`
	ExpiresAt int64  `json:"expires_at"`
}

// ResultsResponse is the response from GET /v1/results.
type ResultsResponse struct {
	Results []ResultBlob `json:"results"`
}

// PollResults long-polls for any result pages for jobID.
func (c *Client) PollResults(ctx context.Context, jobID string, waitMs int) (*ResultsResponse, error) {
	q := url.Values{}
	q.Set("job_id", jobID)
	q.Set("wait_ms", fmt.Sprintf("%d", waitMs))
	var out ResultsResponse
	if err := c.do(ctx, "GET", "/v1/results", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokePair tells the relay to drop this pair's mailbox entirely.
func (c *Client) RevokePair(ctx context.Context) error {
	return c.do(ctx, "DELETE", "/v1/pair", nil, nil, nil)
}

// PairState mirrors the relay's /v1/pair response. Fields are pointers so
// "not yet committed" reads as JSON null.
type PairState struct {
	IOSPub      *string `json:"ios_pub"`
	CLIPub      *string `json:"cli_pub"`
	AuthToken   *string `json:"auth_token"`
	CompletedAt *int64  `json:"completed_at"`
}

// PostPubkey commits one side's X25519 pubkey to the relay's pair record.
// Returns the (possibly partially-filled) pair state.
func (c *Client) PostPubkey(ctx context.Context, side, pubkeyHex string) (*PairState, error) {
	body := map[string]any{"side": side, "pubkey": pubkeyHex}
	var out PairState
	if err := c.do(ctx, "POST", "/v1/pair", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PollPair long-polls the /v1/pair endpoint until both sides have
// committed (or until waitMs elapses).
func (c *Client) PollPair(ctx context.Context, waitMs int) (*PairState, error) {
	q := url.Values{}
	q.Set("wait_ms", fmt.Sprintf("%d", waitMs))
	var out PairState
	if err := c.do(ctx, "GET", "/v1/pair", q, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// do is the lone HTTP plumbing point.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c.PairID == "" {
		return errors.New("relay: PairID not set on client")
	}
	if query == nil {
		query = url.Values{}
	}
	query.Set("pair", c.PairID)
	full := c.BaseURL + path + "?" + query.Encode()

	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("relay: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, full, bodyReader)
	if err != nil {
		return fmt.Errorf("relay: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	req.Header.Set("accept", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("authorization", "Bearer "+c.AuthToken)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("relay: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("relay: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		var e Error
		if jerr := json.Unmarshal(respBody, &e); jerr != nil || e.Code == "" {
			return &Error{
				HTTPStatus: resp.StatusCode,
				Code:       "http_error",
				Message:    string(respBody),
			}
		}
		e.HTTPStatus = resp.StatusCode
		return &e
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("relay: decode response: %w", err)
		}
	}
	return nil
}
