package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/crypto"
)

// stubRelayServer responds to /v1/health with the given payload.
func stubRelayServer(t *testing.T, ok bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestStatusHumanOutput(t *testing.T) {
	dir := t.TempDir()
	relayURL := stubRelayServer(t, true)
	rec := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000040",
		RelayURL:   relayURL,
		AuthToken:  "tok",
		SessionKey: bytes.Repeat([]byte{0xab}, crypto.SessionKeySize),
	}
	if err := config.SavePair(dir, rec); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"status", "--pair", rec.PairID})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v\n%s", err, buf.String())
	}
	out := buf.String()
	for _, want := range []string{rec.PairID, relayURL, "relay_ok : yes"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output missing %q\noutput:\n%s", want, out)
		}
	}
}

func TestStatusReportsRelayDown(t *testing.T) {
	dir := t.TempDir()
	relayURL := stubRelayServer(t, false)
	rec := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000041",
		RelayURL:   relayURL,
		SessionKey: bytes.Repeat([]byte{0xab}, crypto.SessionKeySize),
	}
	if err := config.SavePair(dir, rec); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"status", "--pair", rec.PairID})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(buf.String(), "relay_ok : no") {
		t.Errorf("expected relay_ok=no, got:\n%s", buf.String())
	}
}

func TestStatusJSONOutput(t *testing.T) {
	dir := t.TempDir()
	relayURL := stubRelayServer(t, true)
	rec := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000042",
		RelayURL:   relayURL,
		SessionKey: bytes.Repeat([]byte{0xab}, crypto.SessionKeySize),
	}
	if err := config.SavePair(dir, rec); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", dir)

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"status", "--pair", rec.PairID, "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got["pair_id"] != rec.PairID {
		t.Errorf("pair_id = %v, want %s", got["pair_id"], rec.PairID)
	}
	if got["relay_ok"] != true {
		t.Errorf("relay_ok = %v, want true", got["relay_ok"])
	}
}

func TestStatusUsesDefaultConfigWithoutPairFlag(t *testing.T) {
	base := t.TempDir()
	configRoot := filepath.Join(base, "config")
	dotRoot := filepath.Join(base, ".healthbridge")
	relayURL := stubRelayServer(t, true)
	rec := &config.PairRecord{
		PairID:     "01J9ZX0PAIR000000000000043",
		RelayURL:   relayURL,
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

	root := Root()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"status", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v\n%s", err, buf.String())
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, buf.String())
	}
	if got["pair_id"] != rec.PairID {
		t.Errorf("pair_id = %v, want %s", got["pair_id"], rec.PairID)
	}
	if got["relay_url"] != relayURL {
		t.Errorf("relay_url = %v, want %s", got["relay_url"], relayURL)
	}
}
