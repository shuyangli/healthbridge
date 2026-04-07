package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadPair(t *testing.T) {
	dir := t.TempDir()
	rec := &PairRecord{
		PairID:     "01J9ZX0PAIR000000000000001",
		RelayURL:   "https://relay.example.com",
		AuthToken:  "abc123def456",
		SessionKey: bytes.Repeat([]byte{0xab}, 32),
		IOSPubHex:  "deadbeef",
		CLIPubHex:  "cafebabe",
		CLIPrivHex: "00112233",
	}
	if err := SavePair(dir, rec); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadPair(dir, rec.PairID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PairID != rec.PairID {
		t.Errorf("PairID = %q, want %q", loaded.PairID, rec.PairID)
	}
	if !bytes.Equal(loaded.SessionKey, rec.SessionKey) {
		t.Errorf("session key not preserved")
	}
	if loaded.RelayURL != rec.RelayURL {
		t.Errorf("RelayURL = %q, want %q", loaded.RelayURL, rec.RelayURL)
	}
	if loaded.AuthToken != rec.AuthToken {
		t.Errorf("AuthToken = %q, want %q", loaded.AuthToken, rec.AuthToken)
	}
	if loaded.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set on save")
	}
}

func TestSavePairCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	rec := &PairRecord{
		PairID:     "01J9ZX0PAIR000000000000001",
		SessionKey: bytes.Repeat([]byte{0xab}, 32),
	}
	if err := SavePair(dir, rec); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "pairs"))
	if err != nil {
		t.Fatalf("pairs dir not created: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("pairs dir perm = %o, want 0700", info.Mode().Perm())
	}
}

func TestLoadPairNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadPair(dir, "01J9ZX0PAIR000000000000999")
	if err == nil {
		t.Fatal("expected error for missing pair")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist, got %v", err)
	}
}

func TestSavePairRejectsBadKey(t *testing.T) {
	rec := &PairRecord{PairID: "p", SessionKey: []byte{0x01}}
	err := SavePair(t.TempDir(), rec)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDeletePairIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	if err := DeletePair(dir, "01J9ZX0PAIR000000000000001"); err != nil {
		t.Fatal(err)
	}
	rec := &PairRecord{
		PairID:     "01J9ZX0PAIR000000000000001",
		SessionKey: bytes.Repeat([]byte{0xab}, 32),
	}
	_ = SavePair(dir, rec)
	if err := DeletePair(dir, rec.PairID); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPair(dir, rec.PairID); !os.IsNotExist(err) {
		t.Errorf("expected gone, got %v", err)
	}
}
