package pairing

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/crypto"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

var errPairTimeout = errors.New("pair timeout")

// runPairing runs both halves of the pairing protocol concurrently against
// a single fakerelay and returns the two results. This is the core
// scenario-test helper for the pairing flow.
func runPairing(t *testing.T, pairID string) (*Result, *Result) {
	t.Helper()
	srv := fakerelay.New()
	t.Cleanup(srv.Close)

	iosClient := relay.New(srv.URL(), pairID)
	cliClient := relay.New(srv.URL(), pairID)

	var (
		mu          sync.Mutex
		iosResult   *Result
		cliResult   *Result
		iosErr      error
		cliErr      error
		iosLinkSent = make(chan *Result, 1)
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// iOS side: post pubkey first, then advertise the link to the CLI side
	// (in production this happens via a QR code; in the test it happens via
	// a channel), then long-poll for completion.
	go func() {
		// Phase 1: post the ios pubkey so the relay knows it.
		// We can't use PairAsIOS here because that helper does post + poll
		// in one call; we need to publish the link mid-flight. Inline the
		// two steps.
		keys, err := crypto.GenerateKeyPair()
		if err != nil {
			mu.Lock()
			iosErr = err
			mu.Unlock()
			close(iosLinkSent)
			return
		}
		_, err = iosClient.PostPubkey(ctx, "ios", hex.EncodeToString(keys.Public))
		if err != nil {
			mu.Lock()
			iosErr = err
			mu.Unlock()
			close(iosLinkSent)
			return
		}
		iosLinkSent <- &Result{
			PairID: pairID,
			IOSPub: keys.Public,
		}

		// Phase 2: long-poll for completion and derive the session key.
		state, err := iosClient.PollPair(ctx, 5000)
		if err != nil {
			mu.Lock()
			iosErr = err
			mu.Unlock()
			return
		}
		if state.CLIPub == nil || state.AuthToken == nil {
			mu.Lock()
			iosErr = errPairTimeout
			mu.Unlock()
			return
		}
		cliPub, _ := hex.DecodeString(*state.CLIPub)
		shared, err := crypto.SharedSecret(keys.Private, cliPub)
		if err != nil {
			mu.Lock()
			iosErr = err
			mu.Unlock()
			return
		}
		transcript := crypto.BuildTranscript(pairID, keys.Public, cliPub)
		session, _ := crypto.DeriveSessionKey(shared, transcript, "healthbridge/m2/session")
		mu.Lock()
		iosResult = &Result{
			PairID:     pairID,
			SessionKey: session,
			AuthToken:  *state.AuthToken,
			IOSPub:     keys.Public,
			CLIPub:     cliPub,
			SAS:        crypto.SAS(shared, transcript),
		}
		mu.Unlock()
	}()

	// CLI side: wait for the iOS link, then run the CLI half.
	link := <-iosLinkSent
	if iosErr != nil {
		t.Fatalf("ios setup: %v", iosErr)
	}
	pairLink := &PairLink{
		PairID:   pairID,
		IOSPub:   hex.EncodeToString(link.IOSPub),
		RelayURL: srv.URL(),
		Version:  protocolVersion,
	}
	r, err := PairAsCLI(ctx, cliClient, pairLink)
	if err != nil {
		t.Fatalf("PairAsCLI: %v", err)
	}
	cliResult = r
	cliErr = nil

	// Wait for the iOS goroutine to finish so we can compare.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		if iosResult != nil || iosErr != nil {
			mu.Unlock()
			break
		}
		mu.Unlock()
		select {
		case <-deadline:
			t.Fatal("ios half never finished")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if iosErr != nil {
		t.Fatalf("ios half: %v", iosErr)
	}
	_ = cliErr
	return iosResult, cliResult
}

func TestPairingProducesSameSessionAndSAS(t *testing.T) {
	ios, cli := runPairing(t, "01J9ZX0PAIR000000000000001")

	if !bytes.Equal(ios.SessionKey, cli.SessionKey) {
		t.Errorf("session keys diverged:\n ios %x\n cli %x", ios.SessionKey, cli.SessionKey)
	}
	if ios.SAS != cli.SAS {
		t.Errorf("SAS diverged: ios=%q cli=%q", ios.SAS, cli.SAS)
	}
	if ios.AuthToken != cli.AuthToken {
		t.Errorf("auth_token diverged: ios=%q cli=%q", ios.AuthToken, cli.AuthToken)
	}
	if ios.PairID != cli.PairID {
		t.Errorf("pair_id diverged: ios=%q cli=%q", ios.PairID, cli.PairID)
	}
}

func TestEncodeDecodeLinkRoundTrip(t *testing.T) {
	link := &PairLink{
		PairID:   "01J9ZX0PAIR000000000000001",
		IOSPub:   "deadbeef",
		RelayURL: "https://example.com",
	}
	s, err := EncodeLink(link)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeLink(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.PairID != link.PairID || got.IOSPub != link.IOSPub || got.RelayURL != link.RelayURL {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, link)
	}
	if got.Version != protocolVersion {
		t.Errorf("Version = %q, want %q", got.Version, protocolVersion)
	}
}

func TestDecodeLinkRejectsBadVersion(t *testing.T) {
	bad := `{"v":"v999","pair_id":"x","ios_pub_hex":"deadbeef","relay_url":"https://example.com"}`
	if _, err := DecodeLink(bad); err == nil {
		t.Error("expected version mismatch error")
	}
}

func TestDecodeLinkRejectsMissingFields(t *testing.T) {
	for _, bad := range []string{
		`{"v":"healthbridge-pair-v1"}`,
		`{"v":"healthbridge-pair-v1","pair_id":"x"}`,
		`{"v":"healthbridge-pair-v1","pair_id":"x","ios_pub_hex":"deadbeef"}`,
	} {
		if _, err := DecodeLink(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
