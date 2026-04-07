package jobs

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// _ keep sql package referenced even if no exported types from it survive.
var _ = sql.ErrNoRows

// JobStatus is the lifecycle state of a job in the local mirror.
type JobStatus string

const (
	StatusPending  JobStatus = "pending"
	StatusRunning  JobStatus = "running"
	StatusDone     JobStatus = "done"
	StatusFailed   JobStatus = "failed"
	StatusExpired  JobStatus = "expired"
	StatusCanceled JobStatus = "canceled"
)

// JobRecord is one row in the local mirror. The mirror exists so the user
// can list / poll / wait on jobs the CLI has enqueued, even after the
// process that submitted them has exited. The relay is the source of
// truth for delivery; the mirror is the source of truth for "what has
// this CLI ever asked the iPhone to do".
type JobRecord struct {
	ID           string
	PairID       string
	Kind         string
	Status       JobStatus
	PayloadJSON  []byte
	ResultJSON   []byte
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
	Deadline     time.Time
	CompletedAt  time.Time
	Attempts     int
}

// ListFilter narrows a Store.List query.
type ListFilter struct {
	PairID string
	Status JobStatus
	Since  time.Time
	Limit  int
}

// Store is a SQLite-backed local job mirror. Always opened from a path
// (use ":memory:" for tests).
type Store struct {
	db *sql.DB
}

// Open creates or opens the SQLite database, runs the schema migration,
// and returns a Store. The parent directory is created with 0700 perms.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("jobs: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("jobs: open db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// migrate runs the (currently single) schema version.
func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS jobs (
    id            TEXT PRIMARY KEY,
    pair_id       TEXT NOT NULL,
    kind          TEXT NOT NULL,
    status        TEXT NOT NULL,
    payload_json  BLOB NOT NULL,
    result_json   BLOB,
    error_code    TEXT,
    error_message TEXT,
    created_at    INTEGER NOT NULL,
    deadline      INTEGER,
    completed_at  INTEGER,
    attempts      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS jobs_pair_status ON jobs(pair_id, status);
CREATE INDEX IF NOT EXISTS jobs_created_at  ON jobs(created_at);
`
	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("jobs: migrate: %w", err)
	}
	return nil
}

// Enqueue inserts a new job record. ID and PayloadJSON are required.
// Status defaults to pending; CreatedAt defaults to now.
func (s *Store) Enqueue(rec *JobRecord) error {
	if rec.ID == "" {
		return errors.New("jobs: ID required")
	}
	if rec.PairID == "" {
		return errors.New("jobs: PairID required")
	}
	if len(rec.PayloadJSON) == 0 {
		return errors.New("jobs: PayloadJSON required")
	}
	if rec.Status == "" {
		rec.Status = StatusPending
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
        INSERT INTO jobs (id, pair_id, kind, status, payload_json,
                          created_at, deadline)
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.PairID, rec.Kind, string(rec.Status), rec.PayloadJSON,
		rec.CreatedAt.UnixMilli(), nullableMillis(rec.Deadline),
	)
	if err != nil {
		return fmt.Errorf("jobs: enqueue %s: %w", rec.ID, err)
	}
	return nil
}

// Get fetches a single job by ID. Returns sql.ErrNoRows if missing.
func (s *Store) Get(id string) (*JobRecord, error) {
	row := s.db.QueryRow(`
        SELECT id, pair_id, kind, status, payload_json, result_json,
               COALESCE(error_code, ''), COALESCE(error_message, ''),
               created_at, COALESCE(deadline, 0), COALESCE(completed_at, 0),
               attempts
        FROM jobs WHERE id = ?`, id)
	return scanRow(row)
}

// List returns jobs matching filter, ordered by created_at descending.
func (s *Store) List(f ListFilter) ([]*JobRecord, error) {
	q := `SELECT id, pair_id, kind, status, payload_json, result_json,
                 COALESCE(error_code, ''), COALESCE(error_message, ''),
                 created_at, COALESCE(deadline, 0), COALESCE(completed_at, 0),
                 attempts
          FROM jobs WHERE 1=1`
	args := []any{}
	if f.PairID != "" {
		q += " AND pair_id = ?"
		args = append(args, f.PairID)
	}
	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, string(f.Status))
	}
	if !f.Since.IsZero() {
		q += " AND created_at >= ?"
		args = append(args, f.Since.UnixMilli())
	}
	q += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("jobs: list: %w", err)
	}
	defer rows.Close()

	var out []*JobRecord
	for rows.Next() {
		rec, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Sort defensively in case the index ordering ever drifts.
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// MarkDone updates a job to status=done with the given result blob.
func (s *Store) MarkDone(id string, result []byte) error {
	now := time.Now().UTC().UnixMilli()
	_, err := s.db.Exec(`
        UPDATE jobs SET status = ?, result_json = ?, completed_at = ?,
                        error_code = NULL, error_message = NULL
        WHERE id = ?`,
		string(StatusDone), result, now, id,
	)
	return err
}

// MarkFailed updates a job to status=failed with the structured error.
func (s *Store) MarkFailed(id, code, message string) error {
	now := time.Now().UTC().UnixMilli()
	_, err := s.db.Exec(`
        UPDATE jobs SET status = ?, error_code = ?, error_message = ?,
                        completed_at = ?
        WHERE id = ?`,
		string(StatusFailed), code, message, now, id,
	)
	return err
}

// Cancel marks a pending job as canceled. Non-pending jobs are left alone.
func (s *Store) Cancel(id string) error {
	res, err := s.db.Exec(`
        UPDATE jobs SET status = ?, completed_at = ?
        WHERE id = ? AND status = ?`,
		string(StatusCanceled), time.Now().UTC().UnixMilli(), id, string(StatusPending),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("jobs: cannot cancel %s (not pending)", id)
	}
	return nil
}

// Prune deletes terminal-state jobs older than `olderThan`. Returns the
// number of rows removed.
func (s *Store) Prune(olderThan time.Time) (int, error) {
	res, err := s.db.Exec(`
        DELETE FROM jobs
        WHERE status IN (?, ?, ?, ?)
          AND created_at < ?`,
		string(StatusDone), string(StatusFailed),
		string(StatusExpired), string(StatusCanceled),
		olderThan.UnixMilli(),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// WipePair deletes every job belonging to the given pair, regardless of
// status. Used by `healthbridge wipe`.
func (s *Store) WipePair(pairID string) error {
	_, err := s.db.Exec(`DELETE FROM jobs WHERE pair_id = ?`, pairID)
	return err
}

// ExpireOverdue moves any pending job whose deadline has passed into
// status=expired. Returns the number of rows touched.
func (s *Store) ExpireOverdue(now time.Time) (int, error) {
	res, err := s.db.Exec(`
        UPDATE jobs SET status = ?, completed_at = ?
        WHERE status = ? AND deadline IS NOT NULL AND deadline < ?`,
		string(StatusExpired), now.UnixMilli(),
		string(StatusPending), now.UnixMilli(),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ---- scanning helpers --------------------------------------------------

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(s rowScanner) (*JobRecord, error) {
	rec := &JobRecord{}
	var status string
	var createdAt, deadline, completedAt int64
	// resultJSON is sometimes NULL; use a sql.NullString proxy.
	var resultJSON []byte
	if err := s.Scan(
		&rec.ID, &rec.PairID, &rec.Kind, &status, &rec.PayloadJSON,
		&resultJSON, &rec.ErrorCode, &rec.ErrorMessage,
		&createdAt, &deadline, &completedAt, &rec.Attempts,
	); err != nil {
		return nil, err
	}
	rec.Status = JobStatus(status)
	rec.CreatedAt = time.UnixMilli(createdAt).UTC()
	if deadline > 0 {
		rec.Deadline = time.UnixMilli(deadline).UTC()
	}
	if completedAt > 0 {
		rec.CompletedAt = time.UnixMilli(completedAt).UTC()
	}
	if len(resultJSON) > 0 {
		rec.ResultJSON = resultJSON
	}
	return rec, nil
}

func scanRows(rows *sql.Rows) (*JobRecord, error) { return scanRow(rows) }

func nullableMillis(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}
