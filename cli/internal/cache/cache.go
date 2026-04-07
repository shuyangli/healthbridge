// Package cache is the CLI's local mirror of HealthKit samples plus the
// per-type sync anchors. It is populated by `healthbridge sync` via the
// HKAnchoredObjectQuery delta protocol; the iPhone is still the source of
// truth, but the cache lets the agent answer "what data has the user ever
// shared with me" without round-tripping.
//
// As of M4 the cache stores samples but does not yet expose query helpers
// — the read subcommand always goes through the iPhone. Reading from
// cache is a future enhancement.
package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"github.com/shuyangli/healthbridge/cli/internal/health"
)

// Cache is the SQLite-backed sample + anchor store.
type Cache struct {
	db *sql.DB
}

// Open creates or opens the cache file. Use ":memory:" for tests.
func Open(path string) (*Cache, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("cache: mkdir: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("cache: open db: %w", err)
	}
	c := &Cache{db: db}
	if err := c.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

// Close releases the database handle.
func (c *Cache) Close() error { return c.db.Close() }

func (c *Cache) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS samples (
    uuid             TEXT NOT NULL,
    pair_id          TEXT NOT NULL,
    sample_type      TEXT NOT NULL,
    value            REAL NOT NULL,
    unit             TEXT NOT NULL,
    start_at         INTEGER NOT NULL,
    end_at           INTEGER NOT NULL,
    metadata_json    BLOB,
    source_name      TEXT,
    source_bundle_id TEXT,
    PRIMARY KEY (pair_id, uuid)
);
CREATE INDEX IF NOT EXISTS samples_type_start
  ON samples(pair_id, sample_type, start_at DESC);

CREATE TABLE IF NOT EXISTS anchors (
    pair_id     TEXT NOT NULL,
    sample_type TEXT NOT NULL,
    anchor      BLOB,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (pair_id, sample_type)
);
`
	_, err := c.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("cache: migrate: %w", err)
	}
	return nil
}

// GetAnchor returns the per-type anchor blob for this pair, or nil if no
// previous sync has happened. Callers send a missing-anchor request to
// the iPhone for a full sync of that type.
func (c *Cache) GetAnchor(pairID, sampleType string) ([]byte, error) {
	var anchor []byte
	err := c.db.QueryRow(`
        SELECT anchor FROM anchors WHERE pair_id = ? AND sample_type = ?`,
		pairID, sampleType,
	).Scan(&anchor)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cache: get anchor: %w", err)
	}
	return anchor, nil
}

// SetAnchor persists a new anchor for (pair, type). Idempotent.
func (c *Cache) SetAnchor(pairID, sampleType string, anchor []byte) error {
	_, err := c.db.Exec(`
        INSERT INTO anchors (pair_id, sample_type, anchor, updated_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(pair_id, sample_type) DO UPDATE
        SET anchor = excluded.anchor, updated_at = excluded.updated_at`,
		pairID, sampleType, anchor, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("cache: set anchor: %w", err)
	}
	return nil
}

// AllAnchors returns the current set of anchors for pair, used by
// `healthbridge sync` to build the request payload.
func (c *Cache) AllAnchors(pairID string) (map[string][]byte, error) {
	rows, err := c.db.Query(`
        SELECT sample_type, anchor FROM anchors WHERE pair_id = ?`, pairID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]byte)
	for rows.Next() {
		var t string
		var a []byte
		if err := rows.Scan(&t, &a); err != nil {
			return nil, err
		}
		out[t] = a
	}
	return out, rows.Err()
}

// ApplyAdds inserts or replaces a batch of samples in a single transaction.
// The samples must already have UUIDs assigned by HealthKit on the iOS side.
func (c *Cache) ApplyAdds(pairID string, samples []health.Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`
        INSERT INTO samples (uuid, pair_id, sample_type, value, unit,
                             start_at, end_at, metadata_json,
                             source_name, source_bundle_id)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(pair_id, uuid) DO UPDATE SET
            sample_type      = excluded.sample_type,
            value            = excluded.value,
            unit             = excluded.unit,
            start_at         = excluded.start_at,
            end_at           = excluded.end_at,
            metadata_json    = excluded.metadata_json,
            source_name      = excluded.source_name,
            source_bundle_id = excluded.source_bundle_id`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, s := range samples {
		if s.UUID == "" {
			return fmt.Errorf("cache: sample missing UUID: %+v", s)
		}
		var meta []byte
		if s.Metadata != nil {
			meta, _ = json.Marshal(s.Metadata)
		}
		var srcName, srcBundle any
		if s.Source != nil {
			srcName = s.Source.Name
			srcBundle = s.Source.BundleID
		}
		if _, err := stmt.Exec(
			s.UUID, pairID, string(s.Type), s.Value, s.Unit,
			s.Start.UnixMilli(), s.End.UnixMilli(), meta,
			srcName, srcBundle,
		); err != nil {
			return fmt.Errorf("cache: insert sample %s: %w", s.UUID, err)
		}
	}
	return tx.Commit()
}

// ApplyDeletes removes the listed UUIDs from the cache for this pair.
func (c *Cache) ApplyDeletes(pairID string, uuids []string) error {
	if len(uuids) == 0 {
		return nil
	}
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`DELETE FROM samples WHERE pair_id = ? AND uuid = ?`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, u := range uuids {
		if _, err := stmt.Exec(pairID, u); err != nil {
			return fmt.Errorf("cache: delete %s: %w", u, err)
		}
	}
	return tx.Commit()
}

// SampleCount reports how many samples are cached for this pair (any type).
func (c *Cache) SampleCount(pairID string) (int, error) {
	var n int
	err := c.db.QueryRow(`SELECT COUNT(*) FROM samples WHERE pair_id = ?`, pairID).Scan(&n)
	return n, err
}

// SampleCountByType reports how many of a particular sample type are cached.
func (c *Cache) SampleCountByType(pairID, sampleType string) (int, error) {
	var n int
	err := c.db.QueryRow(`
        SELECT COUNT(*) FROM samples WHERE pair_id = ? AND sample_type = ?`,
		pairID, sampleType,
	).Scan(&n)
	return n, err
}

// Wipe removes everything for this pair (samples + anchors). Used by
// `healthbridge wipe` and by `healthbridge sync --full` to reset a type.
func (c *Cache) Wipe(pairID string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM samples WHERE pair_id = ?`, pairID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM anchors WHERE pair_id = ?`, pairID); err != nil {
		return err
	}
	return tx.Commit()
}

// WipeType removes the anchor and samples for a specific type. Used by
// `healthbridge sync --full --type body_mass`.
func (c *Cache) WipeType(pairID, sampleType string) error {
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`DELETE FROM samples WHERE pair_id = ? AND sample_type = ?`,
		pairID, sampleType,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`DELETE FROM anchors WHERE pair_id = ? AND sample_type = ?`,
		pairID, sampleType,
	); err != nil {
		return err
	}
	return tx.Commit()
}
