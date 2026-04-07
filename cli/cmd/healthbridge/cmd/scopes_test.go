package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/crypto"
)

// seedPair writes a minimal pair record to a temp config dir and returns
// the dir + the pair_id, ready for a `healthbridge scopes ...` command.
func seedPair(t *testing.T, scopes []string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	pairID := "01J9ZX0PAIR000000000000030"
	rec := &config.PairRecord{
		PairID:     pairID,
		RelayURL:   "https://relay.example.com",
		AuthToken:  "test-token",
		SessionKey: bytes.Repeat([]byte{0xab}, crypto.SessionKeySize),
		Scopes:     scopes,
	}
	if err := config.SavePair(dir, rec); err != nil {
		t.Fatal(err)
	}
	return dir, pairID
}

func runScopes(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestScopesListDefaultGrantsAll(t *testing.T) {
	dir, pairID := seedPair(t, nil)
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)
	out, err := runScopes(t, "scopes", "list", "--pair", pairID)
	if err != nil {
		t.Fatalf("runScopes: %v\n%s", err, out)
	}
	if !strings.Contains(out, "all sample types granted") {
		t.Errorf("expected 'all sample types granted' header, got:\n%s", out)
	}
}

func TestScopesGrantThenList(t *testing.T) {
	dir, pairID := seedPair(t, []string{"step_count"})
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)

	if _, err := runScopes(t, "scopes", "grant", "dietary_energy_consumed", "--pair", pairID); err != nil {
		t.Fatalf("grant: %v", err)
	}
	out, err := runScopes(t, "scopes", "list", "--pair", pairID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "step_count") || !strings.Contains(out, "dietary_energy_consumed") {
		t.Errorf("expected both grants in list, got:\n%s", out)
	}
}

func TestScopesGrantRejectsUnknownType(t *testing.T) {
	dir, pairID := seedPair(t, nil)
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)
	_, err := runScopes(t, "scopes", "grant", "totally_made_up", "--pair", pairID)
	if err == nil {
		t.Fatal("expected error for unknown sample type")
	}
}

func TestScopesRevokeNarrowsFromAll(t *testing.T) {
	dir, pairID := seedPair(t, nil)
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)
	if _, err := runScopes(t, "scopes", "revoke", "step_count", "--pair", pairID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	rec, err := config.LoadPair(dir, pairID)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range rec.Scopes {
		if s == "step_count" {
			t.Error("step_count should have been revoked")
		}
	}
	// Should still have all the other types.
	if len(rec.Scopes) == 0 {
		t.Error("expected non-empty scope list after first revoke")
	}
}

func TestPairRecordHasScope(t *testing.T) {
	rec := &config.PairRecord{}
	if !rec.HasScope("step_count") {
		t.Error("empty scopes should grant everything")
	}
	rec.Scopes = []string{"step_count"}
	if !rec.HasScope("step_count") {
		t.Error("explicit grant should match")
	}
	if rec.HasScope("body_mass") {
		t.Error("non-granted type should be rejected")
	}
}
