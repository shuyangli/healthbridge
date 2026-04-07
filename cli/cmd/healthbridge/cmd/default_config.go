package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shuyangli/healthbridge/cli/internal/config"
)

type defaultConfig struct {
	PairID   string `json:"pair_id,omitempty"`
	RelayURL string `json:"relay_url,omitempty"`
}

func healthbridgeHomeDir() string {
	if v := os.Getenv("HEALTHBRIDGE_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".healthbridge"
	}
	return filepath.Join(home, ".healthbridge")
}

func defaultConfigPath() string {
	return filepath.Join(healthbridgeHomeDir(), "config")
}

func loadDefaultConfig() (*defaultConfig, error) {
	data, err := os.ReadFile(defaultConfigPath())
	if err != nil {
		return nil, err
	}
	var cfg defaultConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("default config: parse: %w", err)
	}
	return &cfg, nil
}

func saveDefaultConfig(rec *config.PairRecord) error {
	cfg := defaultConfig{
		PairID:   rec.PairID,
		RelayURL: rec.RelayURL,
	}
	if err := os.MkdirAll(healthbridgeHomeDir(), 0o700); err != nil {
		return fmt.Errorf("default config: mkdir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("default config: marshal: %w", err)
	}
	if err := os.WriteFile(defaultConfigPath(), data, 0o600); err != nil {
		return fmt.Errorf("default config: write: %w", err)
	}
	return nil
}
