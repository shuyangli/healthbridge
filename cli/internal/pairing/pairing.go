// Package pairing implements the M2 X25519 + relay /pair flow.
//
// The CLI side runs PairAsCLI: it parses the QR-encoded pair link from the
// iOS app, generates its own X25519 keypair, posts the pubkey to the relay,
// derives the session key + SAS, and returns a Result the caller can show
// to the user (so they can confirm SAS) and then persist via config.SavePair.
//
// The iOS side runs PairAsIOS: it generates a pair_id + keypair, posts its
// pubkey first, then long-polls /v1/pair until the CLI commits its pubkey,
// then derives the same session key + SAS. The kit's Swift mirror lives in
// HealthBridgeKit/Pairing.swift and produces byte-identical output.
//
// Both helpers are pure functions over a relay.Client and the various
// pieces of state. They do not touch the filesystem; persistence is the
// caller's job.
package pairing

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/shuyangli/healthbridge/cli/internal/crypto"
	"github.com/shuyangli/healthbridge/cli/internal/relay"
)

// PairLink is the JSON the iOS app encodes into a QR code at the start of
// pairing. The CLI parses one of these and uses it to drive PairAsCLI.
type PairLink struct {
	PairID   string `json:"pair_id"`
	IOSPub   string `json:"ios_pub_hex"`
	RelayURL string `json:"relay_url"`
	// Version is bumped when the pairing protocol changes. Mismatched
	// versions abort pairing rather than silently produce wrong session
	// keys.
	Version string `json:"v"`
}

const protocolVersion = "healthbridge-pair-v1"

// EncodeLink renders a PairLink as a compact JSON string suitable for a
// QR code or pasted token.
func EncodeLink(link *PairLink) (string, error) {
	if link.Version == "" {
		link.Version = protocolVersion
	}
	b, err := json.Marshal(link)
	if err != nil {
		return "", fmt.Errorf("pairing: marshal link: %w", err)
	}
	return string(b), nil
}

// DecodeLink parses a JSON string back into a PairLink and validates the
// version.
func DecodeLink(s string) (*PairLink, error) {
	var link PairLink
	if err := json.Unmarshal([]byte(s), &link); err != nil {
		return nil, fmt.Errorf("pairing: parse link: %w", err)
	}
	if link.Version != protocolVersion {
		return nil, fmt.Errorf("pairing: unsupported protocol version %q", link.Version)
	}
	if link.PairID == "" || link.IOSPub == "" || link.RelayURL == "" {
		return nil, errors.New("pairing: link missing required fields")
	}
	if _, err := hex.DecodeString(link.IOSPub); err != nil {
		return nil, fmt.Errorf("pairing: ios_pub not hex: %w", err)
	}
	return &link, nil
}

// Result is what either side ends up with after a successful pairing.
type Result struct {
	PairID     string
	RelayURL   string
	SessionKey []byte
	AuthToken  string
	IOSPub     []byte
	CLIPub     []byte
	CLIPriv    []byte // CLI side only; iOS side leaves this nil
	SAS        string
}

// PairAsCLI runs the pairing protocol from the desktop side. It posts the
// CLI pubkey to the relay, retrieves the iOS pubkey + auth_token, derives
// the session key, and returns a Result containing everything the user
// (and the config layer) needs to remember.
func PairAsCLI(ctx context.Context, c *relay.Client, link *PairLink) (*Result, error) {
	if c.PairID != link.PairID {
		return nil, fmt.Errorf("pairing: client pair_id %q != link pair_id %q", c.PairID, link.PairID)
	}
	iosPub, err := hex.DecodeString(link.IOSPub)
	if err != nil {
		return nil, fmt.Errorf("pairing: bad ios_pub: %w", err)
	}
	if len(iosPub) != crypto.PublicKeySize {
		return nil, fmt.Errorf("pairing: ios_pub size %d, want %d", len(iosPub), crypto.PublicKeySize)
	}

	cliKeys, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("pairing: gen cli keypair: %w", err)
	}

	state, err := c.PostPubkey(ctx, "cli", hex.EncodeToString(cliKeys.Public))
	if err != nil {
		return nil, fmt.Errorf("pairing: post cli pubkey: %w", err)
	}
	if state.AuthToken == nil {
		return nil, errors.New("pairing: relay did not issue auth_token (iOS side missing?)")
	}

	shared, err := crypto.SharedSecret(cliKeys.Private, iosPub)
	if err != nil {
		return nil, fmt.Errorf("pairing: shared secret: %w", err)
	}
	transcript := crypto.BuildTranscript(link.PairID, iosPub, cliKeys.Public)
	session, err := crypto.DeriveSessionKey(shared, transcript, "healthbridge/m2/session")
	if err != nil {
		return nil, fmt.Errorf("pairing: derive session: %w", err)
	}

	return &Result{
		PairID:     link.PairID,
		RelayURL:   link.RelayURL,
		SessionKey: session,
		AuthToken:  *state.AuthToken,
		IOSPub:     iosPub,
		CLIPub:     cliKeys.Public,
		CLIPriv:    cliKeys.Private,
		SAS:        crypto.SAS(shared, transcript),
	}, nil
}

// PairAsIOS runs the pairing protocol from the iOS side. It generates a
// keypair, posts the iOS pubkey, long-polls until the CLI commits its
// pubkey, derives the session key, and returns a Result. Used by the
// HealthBridgeKit Swift tests' Go-side counterpart and by Go-only end-to-end
// tests; the production iOS app has its own Swift mirror in
// HealthBridgeKit/Pairing.swift.
func PairAsIOS(ctx context.Context, c *relay.Client, pairID, relayURL string, waitMs int) (*Result, error) {
	keys, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("pairing: gen ios keypair: %w", err)
	}

	if _, err := c.PostPubkey(ctx, "ios", hex.EncodeToString(keys.Public)); err != nil {
		return nil, fmt.Errorf("pairing: post ios pubkey: %w", err)
	}

	state, err := c.PollPair(ctx, waitMs)
	if err != nil {
		return nil, fmt.Errorf("pairing: poll pair: %w", err)
	}
	if state.CLIPub == nil || state.AuthToken == nil {
		return nil, errors.New("pairing: timed out waiting for cli pubkey")
	}
	cliPub, err := hex.DecodeString(*state.CLIPub)
	if err != nil {
		return nil, fmt.Errorf("pairing: bad cli pubkey: %w", err)
	}

	shared, err := crypto.SharedSecret(keys.Private, cliPub)
	if err != nil {
		return nil, err
	}
	transcript := crypto.BuildTranscript(pairID, keys.Public, cliPub)
	session, err := crypto.DeriveSessionKey(shared, transcript, "healthbridge/m2/session")
	if err != nil {
		return nil, err
	}
	return &Result{
		PairID:     pairID,
		RelayURL:   relayURL,
		SessionKey: session,
		AuthToken:  *state.AuthToken,
		IOSPub:     keys.Public,
		CLIPub:     cliPub,
		SAS:        crypto.SAS(shared, transcript),
	}, nil
}
