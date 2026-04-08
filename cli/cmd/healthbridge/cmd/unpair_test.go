package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/crypto"
)

func TestUnpairRemovesRecordAndDefaultConfig(t *testing.T) {
	base := t.TempDir()
	configRoot := filepath.Join(base, "config")
	dotRoot := filepath.Join(base, ".healthbridge")
	rec := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000901",
		RelayURL:   "https://example.invalid",
		SessionKey: bytes.Repeat([]byte{0xab}, crypto.SessionKeySize),
	}
	if err := config.SavePair(configRoot, rec); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", configRoot)
	t.Setenv("HEALTHBRIDGE_HOME", dotRoot)
	if err := saveDefaultConfig(rec); err != nil {
		t.Fatal(err)
	}

	// Sanity: both pieces exist before unpair.
	pairPath := filepath.Join(configRoot, "pairs", rec.PairID+".json")
	if _, err := os.Stat(pairPath); err != nil {
		t.Fatalf("pre: pair record missing: %v", err)
	}
	if _, err := os.Stat(defaultConfigPath()); err != nil {
		t.Fatalf("pre: default config missing: %v", err)
	}

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"unpair", "--pair", rec.PairID, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unpair: %v\n%s", err, buf.String())
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got["pair_record_removed"] != true {
		t.Errorf("pair_record_removed = %v, want true", got["pair_record_removed"])
	}
	if got["default_cleared"] != true {
		t.Errorf("default_cleared = %v, want true", got["default_cleared"])
	}

	if _, err := os.Stat(pairPath); !os.IsNotExist(err) {
		t.Errorf("pair record still present after unpair: %v", err)
	}
	if _, err := os.Stat(defaultConfigPath()); !os.IsNotExist(err) {
		t.Errorf("default config still present after unpair: %v", err)
	}
}

func TestUnpairLeavesUnrelatedDefaultUntouched(t *testing.T) {
	base := t.TempDir()
	configRoot := filepath.Join(base, "config")
	dotRoot := filepath.Join(base, ".healthbridge")

	other := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000902",
		RelayURL:   "https://example.invalid",
		SessionKey: bytes.Repeat([]byte{0xcd}, crypto.SessionKeySize),
	}
	target := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000903",
		RelayURL:   "https://example.invalid",
		SessionKey: bytes.Repeat([]byte{0xef}, crypto.SessionKeySize),
	}
	if err := config.SavePair(configRoot, other); err != nil {
		t.Fatal(err)
	}
	if err := config.SavePair(configRoot, target); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", configRoot)
	t.Setenv("HEALTHBRIDGE_HOME", dotRoot)
	// Active default points at the OTHER pair, not the one we're
	// unpairing.
	if err := saveDefaultConfig(other); err != nil {
		t.Fatal(err)
	}

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"unpair", "--pair", target.PairID, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unpair: %v\n%s", err, buf.String())
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got["pair_record_removed"] != true {
		t.Errorf("pair_record_removed = %v, want true", got["pair_record_removed"])
	}
	if got["default_cleared"] != false {
		t.Errorf("default_cleared = %v, want false (it pointed at a different pair)", got["default_cleared"])
	}

	// The default should still exist and still point at `other`.
	cfg, err := loadDefaultConfig()
	if err != nil {
		t.Fatalf("default config gone after unrelated unpair: %v", err)
	}
	if cfg.PairID != other.PairID {
		t.Errorf("default pair_id = %s, want %s (untouched)", cfg.PairID, other.PairID)
	}

	// And the OTHER pair record should still be on disk.
	otherPath := filepath.Join(configRoot, "pairs", other.PairID+".json")
	if _, err := os.Stat(otherPath); err != nil {
		t.Errorf("unrelated pair record gone: %v", err)
	}
}

func TestUnpairIsIdempotent(t *testing.T) {
	base := t.TempDir()
	configRoot := filepath.Join(base, "config")
	dotRoot := filepath.Join(base, ".healthbridge")
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", configRoot)
	t.Setenv("HEALTHBRIDGE_HOME", dotRoot)

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"unpair", "--pair", "01J9ZX0PAIR000000000000904", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("unpair on empty home should succeed: %v\n%s", err, buf.String())
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got["pair_record_removed"] != false {
		t.Errorf("pair_record_removed = %v, want false", got["pair_record_removed"])
	}
	if got["default_cleared"] != false {
		t.Errorf("default_cleared = %v, want false", got["default_cleared"])
	}
}
