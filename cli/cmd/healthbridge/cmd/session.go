package cmd

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/crypto"
	"github.com/shuyangli/healthbridge/cli/internal/jobs"
)

// loadSession assembles a jobs.Session for the current pair, looking in
// (in priority order):
//
//   1. HEALTHBRIDGE_KEY env var (32-byte hex; useful for tests and CI)
//   2. ~/.config/healthbridge/pairs/<pair_id>.json written by `pair`
//
// Returns a clear error if no session is available.
func loadSession(f commonFlags) (*jobs.Session, error) {
	if hexKey := os.Getenv("HEALTHBRIDGE_KEY"); hexKey != "" {
		key, err := hex.DecodeString(hexKey)
		if err != nil {
			return nil, fmt.Errorf("HEALTHBRIDGE_KEY is not valid hex: %w", err)
		}
		if len(key) != crypto.SessionKeySize {
			return nil, fmt.Errorf("HEALTHBRIDGE_KEY must decode to %d bytes, got %d", crypto.SessionKeySize, len(key))
		}
		return &jobs.Session{Key: key, PairID: f.PairID}, nil
	}

	// Look for a stored pair record. config.LoadPair returns NotFound if
	// the file doesn't exist; we map that to a friendly "run pair first".
	rec, err := config.LoadPair(configDir(), f.PairID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no session for pair %s — run `healthbridge pair` first", f.PairID)
		}
		return nil, fmt.Errorf("load pair: %w", err)
	}
	return &jobs.Session{Key: rec.SessionKey, PairID: rec.PairID}, nil
}

// configDir returns the directory where pair records and config live.
// Falls back to ~/.config/healthbridge if XDG_CONFIG_HOME is not set.
func configDir() string {
	if v := os.Getenv("HEALTHBRIDGE_CONFIG_DIR"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "healthbridge")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".healthbridge"
	}
	return filepath.Join(home, ".config", "healthbridge")
}
