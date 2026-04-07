package cmd

import (
	"encoding/json"

	"github.com/shuyangli/healthbridge/cli/internal/health"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
)

// mirrorEnqueue inserts a row into the local job mirror for a job that is
// about to be sent to the relay. The mirror is optional — passing a nil
// store is a no-op so unit tests that don't care about persistence can
// skip the SQLite plumbing entirely.
func mirrorEnqueue(store *jobs.Store, job *health.Job, pairID string) error {
	if store == nil {
		return nil
	}
	payload, err := json.Marshal(job.Payload)
	if err != nil {
		return err
	}
	return store.Enqueue(&jobs.JobRecord{
		ID:          job.ID,
		PairID:      pairID,
		Kind:        string(job.Kind),
		PayloadJSON: payload,
		CreatedAt:   job.CreatedAt,
		Deadline:    job.Deadline,
	})
}

// mirrorComplete records a terminal-state result for a previously-enqueued
// job. Errors here are intentionally swallowed: a missing mirror row is
// not worth surfacing to the agent if the actual relay round-trip
// succeeded.
func mirrorComplete(store *jobs.Store, jobID string, result *health.Result) {
	if store == nil {
		return
	}
	if result.Status == health.StatusFailed {
		code, msg := "unknown", ""
		if result.Error != nil {
			code = result.Error.Code
			msg = result.Error.Message
		}
		_ = store.MarkFailed(jobID, code, msg)
		return
	}
	blob, _ := json.Marshal(result.Result)
	_ = store.MarkDone(jobID, blob)
}
