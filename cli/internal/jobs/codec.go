// Package jobs handles building, encoding, and decoding the job/result
// blobs that flow through the relay.
//
// As of M2 every blob is encrypted with the per-pair session key
// established at pairing time. The wire format inside a blob is:
//
//     base64( nonce(12) || ChaCha20-Poly1305(json, key, AAD) || tag(16) )
//
// The plaintext is the JSON-marshalled Job or Result envelope. AAD binds
// the blob to (pair_id, job_id[, page_index]) so the relay cannot replay
// a valid blob across pairs or jobs.
//
// Callers always go through a Session, which carries the key + pair_id.
package jobs

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/crypto"
	"github.com/shuyangli/healthbridge/cli/internal/health"
)

// Session holds the per-pair state needed to seal and open blobs.
// In V1 production code one Session is built at pairing time and reused
// across every CLI invocation; in tests it is constructed inline.
type Session struct {
	// Key is the 32-byte session key derived via HKDF from the X25519
	// shared secret + transcript. See cli/internal/crypto.DeriveSessionKey.
	Key []byte
	// PairID is the relay-side mailbox identifier; bound into every AAD.
	PairID string
}

// NewID returns a fresh job ID. We use 16 bytes of randomness rendered as
// hex (32 chars). The relay only requires uniqueness within a pair, so any
// stable random string works.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("jobs: rand.Read failed: %v", err))
	}
	return fmt.Sprintf("%x", b)
}

// SealJob encrypts a Job into the relay-blob format. The job's ID is used
// for AAD binding; callers should set j.ID (or leave it empty for an
// auto-generated one).
func (s *Session) SealJob(j *health.Job) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	if j.ID == "" {
		j.ID = NewID()
	}
	plaintext, err := json.Marshal(j)
	if err != nil {
		return "", fmt.Errorf("jobs: marshal job: %w", err)
	}
	sealed, err := crypto.Seal(s.Key, plaintext, jobAAD(s.PairID, j.ID))
	if err != nil {
		return "", fmt.Errorf("jobs: seal job: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// OpenJob is the inverse of SealJob. The job_id is supplied externally
// because it comes from the relay's plaintext envelope, not from the
// encrypted blob.
func (s *Session) OpenJob(jobID, blob string) (*health.Job, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("jobs: base64 decode: %w", err)
	}
	plaintext, err := crypto.Open(s.Key, sealed, jobAAD(s.PairID, jobID))
	if err != nil {
		return nil, fmt.Errorf("jobs: open job: %w", err)
	}
	var j health.Job
	if err := json.Unmarshal(plaintext, &j); err != nil {
		return nil, fmt.Errorf("jobs: unmarshal job: %w", err)
	}
	if j.ID != jobID {
		// The envelope said one ID but the encrypted body claims another.
		// This is structurally impossible without key compromise, but we
		// belt-and-brace check it.
		return nil, fmt.Errorf("jobs: envelope/body job_id mismatch")
	}
	return &j, nil
}

// SealResult encrypts a Result page. AAD binds the result to its
// (pair, job, page) tuple so a relay can't replay one job's results as
// another's.
func (s *Session) SealResult(jobID string, pageIndex int, r *health.Result) (string, error) {
	if err := s.validate(); err != nil {
		return "", err
	}
	r.JobID = jobID
	r.PageIndex = pageIndex
	plaintext, err := json.Marshal(r)
	if err != nil {
		return "", fmt.Errorf("jobs: marshal result: %w", err)
	}
	sealed, err := crypto.Seal(s.Key, plaintext, resultAAD(s.PairID, jobID, pageIndex))
	if err != nil {
		return "", fmt.Errorf("jobs: seal result: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// OpenResult is the inverse of SealResult.
func (s *Session) OpenResult(jobID string, pageIndex int, blob string) (*health.Result, error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return nil, fmt.Errorf("jobs: base64 decode result: %w", err)
	}
	plaintext, err := crypto.Open(s.Key, sealed, resultAAD(s.PairID, jobID, pageIndex))
	if err != nil {
		return nil, fmt.Errorf("jobs: open result: %w", err)
	}
	var r health.Result
	if err := json.Unmarshal(plaintext, &r); err != nil {
		return nil, fmt.Errorf("jobs: unmarshal result: %w", err)
	}
	if r.JobID != jobID || r.PageIndex != pageIndex {
		return nil, fmt.Errorf("jobs: envelope/body (job_id,page) mismatch")
	}
	return &r, nil
}

func (s *Session) validate() error {
	if len(s.Key) != crypto.SessionKeySize {
		return errors.New("jobs: session key must be 32 bytes")
	}
	if s.PairID == "" {
		return errors.New("jobs: session has empty pair_id")
	}
	return nil
}

// jobAAD and resultAAD MUST stay byte-identical with the Swift mirror in
// HealthBridgeKit/JobsCodec.swift.
func jobAAD(pairID, jobID string) []byte {
	return []byte("pair=" + pairID + "|job=" + jobID)
}

func resultAAD(pairID, jobID string, pageIndex int) []byte {
	return []byte(fmt.Sprintf("pair=%s|job=%s|page=%d", pairID, jobID, pageIndex))
}

// NewReadJob is unchanged from M1 — it builds the typed envelope; sealing
// happens via Session.SealJob.
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
