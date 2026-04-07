package cmd

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/config"
	"github.com/shuyangli/healthbridge/cli/internal/crypto"
	"github.com/shuyangli/healthbridge/cli/internal/pairing"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// TestPairCommandPersistsSession exercises `healthbridge pair` end-to-end:
// the CLI is the initiator, prints a QR (we capture stdout), and a fake
// "iOS responder" goroutine drives the other side via RespondPairing.
// Afterwards we check that the CLI saved the same session key the iOS
// side derived, and that the printed SAS matches.
func TestPairCommandPersistsSession(t *testing.T) {
	srv := fakerelay.New()
	defer srv.Close()

	tmpHome := t.TempDir()
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", tmpHome)
	t.Setenv("HEALTHBRIDGE_HOME", tmpHome)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Capture the link the CLI emits via the test hook so the iOS
	// responder goroutine can drive RespondPairing against it.
	linkCh := make(chan *pairing.PairLink, 1)
	prevHook := pairLinkEmittedHook
	pairLinkEmittedHook = func(link *pairing.PairLink) { linkCh <- link }
	t.Cleanup(func() { pairLinkEmittedHook = prevHook })

	var (
		mu        sync.Mutex
		iosResult *pairing.Result
		iosErr    error
	)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			mu.Lock()
			iosErr = ctx.Err()
			mu.Unlock()
			return
		case link := <-linkCh:
			iosClient := relay.New(srv.URL(), link.PairID)
			r, err := pairing.RespondPairing(ctx, iosClient, link)
			mu.Lock()
			iosResult, iosErr = r, err
			mu.Unlock()
		}
	}()

	root := Root()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetIn(strings.NewReader("yes\n"))
	root.SetArgs([]string{"pair", "--relay", srv.URL(), "--yes", "--wait", "5s"})
	if err := root.Execute(); err != nil {
		t.Fatalf("pair command: %v\noutput: %s", err, stdout.String())
	}

	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if iosErr != nil {
		t.Fatalf("ios responder: %v", iosErr)
	}
	if iosResult == nil {
		t.Fatal("ios responder produced no result")
	}

	rec, err := config.LoadPair(tmpHome, iosResult.PairID)
	if err != nil {
		t.Fatalf("load pair: %v", err)
	}
	if rec.PairID != iosResult.PairID {
		t.Errorf("pair_id = %q, want %q", rec.PairID, iosResult.PairID)
	}
	if len(rec.SessionKey) != crypto.SessionKeySize {
		t.Errorf("session key length = %d, want %d", len(rec.SessionKey), crypto.SessionKeySize)
	}
	if !bytesEqual(rec.SessionKey, iosResult.SessionKey) {
		t.Errorf("CLI saved a different session key than the iOS side derived")
	}
	cfg, err := loadDefaultConfig()
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	if cfg.PairID != iosResult.PairID {
		t.Errorf("default pair_id = %q, want %q", cfg.PairID, iosResult.PairID)
	}
	if cfg.RelayURL != srv.URL() {
		t.Errorf("default relay_url = %q, want %q", cfg.RelayURL, srv.URL())
	}

	if !strings.Contains(stdout.String(), iosResult.SAS) {
		t.Errorf("CLI output missing SAS %q\noutput:\n%s", iosResult.SAS, stdout.String())
	}
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
