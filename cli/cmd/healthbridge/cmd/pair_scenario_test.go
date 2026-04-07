package cmd

import (
	"bytes"
	"context"
	"encoding/hex"
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

// TestPairCommandPersistsSession exercises the `healthbridge pair`
// subcommand against a fakerelay where a fake "iOS side" goroutine has
// already posted its pubkey. After running the command, the SAS the user
// would have seen is checked, and the on-disk pair record is loaded back
// to verify the session key was persisted correctly.
func TestPairCommandPersistsSession(t *testing.T) {
	srv := fakerelay.New()
	defer srv.Close()

	pairID := "01J9ZX0PAIR000000000000010"
	tmpHome := t.TempDir()
	t.Setenv("HEALTHBRIDGE_CONFIG_DIR", tmpHome)

	// "iOS side" — runs in the background, posts its pubkey, then long-polls
	// for the CLI to complete the exchange so it can derive the same key.
	iosKeys, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	iosClient := relay.New(srv.URL(), pairID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := iosClient.PostPubkey(ctx, "ios", hex.EncodeToString(iosKeys.Public)); err != nil {
		t.Fatalf("ios post: %v", err)
	}

	var (
		mu        sync.Mutex
		iosResult *pairing.Result
	)
	go func() {
		state, err := iosClient.PollPair(ctx, 5000)
		if err != nil || state == nil || state.CLIPub == nil || state.AuthToken == nil {
			return
		}
		cliPub, _ := hex.DecodeString(*state.CLIPub)
		shared, _ := crypto.SharedSecret(iosKeys.Private, cliPub)
		transcript := crypto.BuildTranscript(pairID, iosKeys.Public, cliPub)
		session, _ := crypto.DeriveSessionKey(shared, transcript, "healthbridge/m2/session")
		mu.Lock()
		iosResult = &pairing.Result{
			PairID:     pairID,
			SessionKey: session,
			AuthToken:  *state.AuthToken,
			SAS:        crypto.SAS(shared, transcript),
		}
		mu.Unlock()
	}()

	// Run `healthbridge pair --link <json> --yes` against the same relay.
	link := &pairing.PairLink{
		PairID:   pairID,
		IOSPub:   hex.EncodeToString(iosKeys.Public),
		RelayURL: srv.URL(),
	}
	linkJSON, err := pairing.EncodeLink(link)
	if err != nil {
		t.Fatal(err)
	}

	root := Root()
	var stdout bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stdout)
	root.SetIn(strings.NewReader(""))
	root.SetArgs([]string{
		"pair",
		"--link", linkJSON,
		"--pair", pairID, // satisfies the global --pair requirement, even though pair derives its own
		"--yes",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("pair command: %v\noutput: %s", err, stdout.String())
	}

	// Wait for the iOS goroutine to finish so we can compare the SAS.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := iosResult != nil
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("ios half never finished")
		case <-time.After(10 * time.Millisecond):
		}
	}

	rec, err := config.LoadPair(tmpHome, pairID)
	if err != nil {
		t.Fatalf("load pair: %v", err)
	}
	if rec.PairID != pairID {
		t.Errorf("pair_id = %q, want %q", rec.PairID, pairID)
	}
	if len(rec.SessionKey) != crypto.SessionKeySize {
		t.Errorf("session key length = %d, want %d", len(rec.SessionKey), crypto.SessionKeySize)
	}

	mu.Lock()
	want := iosResult.SessionKey
	mu.Unlock()
	if !bytesEqual(rec.SessionKey, want) {
		t.Errorf("CLI saved a different session key than the iOS side derived")
	}

	// The CLI's printed SAS must match what the iOS side computed.
	mu.Lock()
	wantSAS := iosResult.SAS
	mu.Unlock()
	if !strings.Contains(stdout.String(), wantSAS) {
		t.Errorf("CLI output missing SAS %q\noutput:\n%s", wantSAS, stdout.String())
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
