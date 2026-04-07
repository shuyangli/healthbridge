package pairing

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/shuyangli/healthbridge/cli/internal/relay"
	"github.com/shuyangli/healthbridge/cli/internal/relay/fakerelay"
)

// runPairing drives both halves of the new (CLI-initiator) pairing
// protocol against a fakerelay and returns the two results.
func runPairing(t *testing.T, pairID string) (cli, ios *Result) {
	t.Helper()
	srv := fakerelay.New()
	t.Cleanup(srv.Close)

	cliClient := relay.New(srv.URL(), pairID)
	iosClient := relay.New(srv.URL(), pairID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// CLI side: post pubkey first, generate the link, then long-poll
	// for the iOS side via CompletePairing.
	partial, link, err := InitiatePairing(ctx, cliClient, srv.URL())
	if err != nil {
		t.Fatalf("InitiatePairing: %v", err)
	}

	var (
		mu        sync.Mutex
		iosResult *Result
		iosErr    error
	)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		r, err := RespondPairing(ctx, iosClient, link)
		mu.Lock()
		iosResult, iosErr = r, err
		mu.Unlock()
	}()

	// CLI side: complete now that iOS has been kicked off.
	cliResult, err := CompletePairing(ctx, cliClient, partial, 5000)
	if err != nil {
		t.Fatalf("CompletePairing: %v", err)
	}

	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if iosErr != nil {
		t.Fatalf("RespondPairing: %v", iosErr)
	}
	return cliResult, iosResult
}

func TestPairingProducesSameSessionAndSAS(t *testing.T) {
	cli, ios := runPairing(t, "01J9ZX0PAIR000000000000001")

	if !bytes.Equal(cli.SessionKey, ios.SessionKey) {
		t.Errorf("session keys diverged:\n cli %x\n ios %x", cli.SessionKey, ios.SessionKey)
	}
	if cli.SAS != ios.SAS {
		t.Errorf("SAS diverged: cli=%q ios=%q", cli.SAS, ios.SAS)
	}
	if cli.AuthToken != ios.AuthToken {
		t.Errorf("auth_token diverged: cli=%q ios=%q", cli.AuthToken, ios.AuthToken)
	}
	if cli.PairID != ios.PairID {
		t.Errorf("pair_id diverged: cli=%q ios=%q", cli.PairID, ios.PairID)
	}
	if !bytes.Equal(cli.IOSPub, ios.IOSPub) || !bytes.Equal(cli.CLIPub, ios.CLIPub) {
		t.Errorf("pubkeys diverged")
	}
}

func TestEncodeDecodeLinkRoundTrip(t *testing.T) {
	link := &PairLink{
		PairID:   "01J9ZX0PAIR000000000000001",
		CLIPub:   "deadbeef",
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
	if got.PairID != link.PairID || got.CLIPub != link.CLIPub || got.RelayURL != link.RelayURL {
		t.Errorf("round-trip mismatch: %+v vs %+v", got, link)
	}
	if got.Version != protocolVersion {
		t.Errorf("Version = %q, want %q", got.Version, protocolVersion)
	}
}

func TestDecodeLinkRejectsBadVersion(t *testing.T) {
	bad := `{"v":"v999","pair_id":"x","cli_pub_hex":"deadbeef","relay_url":"https://example.com"}`
	if _, err := DecodeLink(bad); err == nil {
		t.Error("expected version mismatch error")
	}
}

// The previous v1 link shape used `ios_pub_hex`. After bumping to v2 the
// CLI must refuse to decode old links rather than silently producing the
// wrong session key.
func TestDecodeLinkRejectsV1Shape(t *testing.T) {
	v1 := `{"v":"healthbridge-pair-v1","pair_id":"x","ios_pub_hex":"deadbeef","relay_url":"https://example.com"}`
	if _, err := DecodeLink(v1); err == nil {
		t.Error("expected v1 link to be rejected by v2 decoder")
	}
}

func TestDecodeLinkRejectsMissingFields(t *testing.T) {
	for _, bad := range []string{
		`{"v":"healthbridge-pair-v2"}`,
		`{"v":"healthbridge-pair-v2","pair_id":"x"}`,
		`{"v":"healthbridge-pair-v2","pair_id":"x","cli_pub_hex":"deadbeef"}`,
	} {
		if _, err := DecodeLink(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
