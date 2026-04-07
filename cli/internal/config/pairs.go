// Package config persists per-pair state on the local filesystem.
//
// V1 stores pair records as plaintext JSON files under the user's config
// directory. V2 will move the session key into the OS Keychain on macOS;
// for now we keep things simple and document the trust boundary.
package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// PairRecord is the persisted form of a paired CLI<->iPhone relationship.
type PairRecord struct {
	PairID     string    `json:"pair_id"`
	RelayURL   string    `json:"relay_url"`
	SessionKey []byte    `json:"-"`
	SessionHex string    `json:"session_key_hex"`
	IOSPubHex  string    `json:"ios_pub_hex"`
	CLIPubHex  string    `json:"cli_pub_hex"`
	CLIPrivHex string    `json:"cli_priv_hex"`
	CreatedAt  time.Time `json:"created_at"`
}

func pairsDir(configDir string) string {
	return filepath.Join(configDir, "pairs")
}

func pairPath(configDir, pairID string) string {
	return filepath.Join(pairsDir(configDir), pairID+".json")
}

// SavePair writes the record to disk with 0600 perms. Creates the parent
// directory if it does not exist.
func SavePair(configDir string, rec *PairRecord) error {
	if rec.PairID == "" {
		return errors.New("config: PairRecord.PairID required")
	}
	if len(rec.SessionKey) != 32 {
		return errors.New("config: session key must be 32 bytes")
	}
	rec.SessionHex = hex.EncodeToString(rec.SessionKey)
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(pairsDir(configDir), 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(pairPath(configDir, rec.PairID), data, 0o600); err != nil {
		return fmt.Errorf("config: write: %w", err)
	}
	return nil
}

// LoadPair reads a previously-saved record. Returns an os.IsNotExist error
// if the file does not exist; callers should map that to a friendly
// "run `healthbridge pair`" message.
func LoadPair(configDir, pairID string) (*PairRecord, error) {
	data, err := os.ReadFile(pairPath(configDir, pairID))
	if err != nil {
		return nil, err
	}
	var rec PairRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	if rec.SessionHex == "" {
		return nil, errors.New("config: pair record missing session_key_hex")
	}
	key, err := hex.DecodeString(rec.SessionHex)
	if err != nil {
		return nil, fmt.Errorf("config: decode session key: %w", err)
	}
	rec.SessionKey = key
	return &rec, nil
}

// DeletePair removes the on-disk record (used by `healthbridge wipe` and
// when the iOS app revokes a pair).
func DeletePair(configDir, pairID string) error {
	err := os.Remove(pairPath(configDir, pairID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
