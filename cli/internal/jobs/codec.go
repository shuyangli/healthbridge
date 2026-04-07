// Package jobs handles building, encoding, and decoding the job/result
// blobs that flow through the relay.
//
// In M1 there is no encryption: blobs are base64-encoded JSON of the typed
// Job / Result structs in cli/internal/health. M2 wraps the JSON in
// XChaCha20-Poly1305 ciphertext but the public API of this package will
// stay the same — callers see typed Job/Result, not bytes.
package jobs

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

// NewID generates a new opaque job ID. We use a 16-byte random hex token
// for M1; M2 will switch to ULIDs once we add the dependency. The relay
// only requires that the ID is non-empty and unique within a pair, so any
// stable random string will do for now.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is unrecoverable on the platforms we care about.
		panic(fmt.Sprintf("jobs: rand.Read failed: %v", err))
	}
	return fmt.Sprintf("%x", b)
}

// EncodeJob serialises a Job into the blob format the relay carries.
// In M1 this is just `base64(json(job))`.
func EncodeJob(j *health.Job) (string, error) {
	raw, err := json.Marshal(j)
	if err != nil {
		return "", fmt.Errorf("jobs: marshal: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// DecodeJob is the inverse of EncodeJob; the iOS app uses this to read the
// next pending job, and tests use it to assert the contents of an enqueued
// blob.
func DecodeJob(blob string) (*health.Job, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("jobs: base64 decode: %w", err)
	}
	var j health.Job
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("jobs: unmarshal: %w", err)
	}
	return &j, nil
}

// EncodeResult mirrors EncodeJob for results.
func EncodeResult(r *health.Result) (string, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("jobs: marshal result: %w", err)
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

// DecodeResult is the inverse of EncodeResult.
func DecodeResult(blob string) (*health.Result, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("jobs: base64 decode result: %w", err)
	}
	var r health.Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("jobs: unmarshal result: %w", err)
	}
	return &r, nil
}

// NewReadJob builds a Job for a read request. CreatedAt defaults to now.
func NewReadJob(t health.SampleType, from, to time.Time) *health.Job {
	return &health.Job{
		ID:        NewID(),
		Kind:      health.KindRead,
		CreatedAt: time.Now().UTC(),
		Payload: health.ReadPayload{
			Type: t,
			From: from,
			To:   to,
		},
	}
}
