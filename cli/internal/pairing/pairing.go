// Package pairing implements the M2 X25519 + relay /pair flow.
//
// Roles in the new flow:
//
//   - The CLI is the *initiator*. `healthbridge pair` mints a pair_id,
//     generates an X25519 keypair, posts its pubkey to the relay, and
//     prints a QR encoding `{pair_id, cli_pub_hex, relay_url}` to the
//     terminal. It then long-polls the relay for the iOS side.
//
//   - The iOS app is the *responder*. The user opens HealthBridge → Pair,
//     scans the QR with the camera, the app generates its own X25519
//     keypair, posts its pubkey to the relay, derives the shared session
//     key, and shows a 6-digit SAS that the user confirms matches what
//     the CLI is showing.
//
// Both sides end up with the same Result. The relay only ever sees the
// two pubkeys and an opaque per-pair auth_token; it cannot read any
// later jobs or results, which are sealed under the derived session key.
//
// The two helpers in this file (InitiatePairing and RespondPairing) are
// pure functions over a relay.Client; persistence is the caller's job.
// HealthBridgeKit/Pairing.swift is a byte-identical Swift mirror.
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

// PairLink is the JSON the CLI encodes into a QR code at the start of
// pairing. The iOS app scans one of these and uses it to drive
// RespondPairing.
type PairLink struct {
	PairID   string `json:"pair_id"`
	CLIPub   string `json:"cli_pub_hex"`
	RelayURL string `json:"relay_url"`
	// Version is bumped when the pairing protocol changes. Mismatched
	// versions abort pairing rather than silently produce wrong session
	// keys.
	Version string `json:"v"`
}

// protocolVersion identifies the current pairing wire format. This was
// bumped from v1 to v2 when the initiator role moved from iOS to CLI;
// the field shape changed (`ios_pub_hex` → `cli_pub_hex`) so old links
// must fail loudly rather than silently produce a wrong session key.
const protocolVersion = "healthbridge-pair-v2"

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
	if link.PairID == "" || link.CLIPub == "" || link.RelayURL == "" {
		return nil, errors.New("pairing: link missing required fields")
	}
	if _, err := hex.DecodeString(link.CLIPub); err != nil {
		return nil, fmt.Errorf("pairing: cli_pub not hex: %w", err)
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
	// Private key of the local side. CLI side fills CLIPriv; iOS side
	// fills IOSPriv. The other field is left nil. We persist the
	// initiator's private key only because it must survive across the
	// long-poll wait for the responder.
	CLIPriv []byte
	IOSPriv []byte
	SAS     string
}

// InitiatePairing runs the CLI (initiator) half of the protocol. It
// generates a fresh X25519 keypair, commits the pubkey to the relay, and
// returns a PairLink the caller should encode into a QR for the iOS app
// to scan. The Result returned at this stage has CLIPriv populated but
// no SessionKey/SAS — those come from CompletePairing once the iOS side
// has responded.
func InitiatePairing(ctx context.Context, c *relay.Client, relayURL string) (*Result, *PairLink, error) {
	if c.PairID == "" {
		return nil, nil, errors.New("pairing: client PairID not set")
	}
	keys, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, nil, fmt.Errorf("pairing: gen cli keypair: %w", err)
	}
	if _, err := c.PostPubkey(ctx, "cli", hex.EncodeToString(keys.Public)); err != nil {
		return nil, nil, fmt.Errorf("pairing: post cli pubkey: %w", err)
	}
	r := &Result{
		PairID:   c.PairID,
		RelayURL: relayURL,
		CLIPub:   keys.Public,
		CLIPriv:  keys.Private,
	}
	link := &PairLink{
		PairID:   c.PairID,
		CLIPub:   hex.EncodeToString(keys.Public),
		RelayURL: relayURL,
		Version:  protocolVersion,
	}
	return r, link, nil
}

// CompletePairing finishes the CLI (initiator) side after the iOS app
// has scanned the QR and posted its pubkey. It long-polls the relay for
// the iOS pubkey + auth_token, derives the session key, and fills in
// SessionKey/AuthToken/IOSPub/SAS on the Result returned by InitiatePairing.
func CompletePairing(ctx context.Context, c *relay.Client, partial *Result, waitMs int) (*Result, error) {
	if partial == nil || len(partial.CLIPriv) == 0 || len(partial.CLIPub) == 0 {
		return nil, errors.New("pairing: CompletePairing requires a partial Result from InitiatePairing")
	}
	state, err := c.PollPair(ctx, waitMs)
	if err != nil {
		return nil, fmt.Errorf("pairing: poll pair: %w", err)
	}
	if state.IOSPub == nil || state.AuthToken == nil {
		return nil, errors.New("pairing: timed out waiting for ios pubkey")
	}
	iosPub, err := hex.DecodeString(*state.IOSPub)
	if err != nil {
		return nil, fmt.Errorf("pairing: bad ios pubkey: %w", err)
	}
	if len(iosPub) != crypto.PublicKeySize {
		return nil, fmt.Errorf("pairing: ios pubkey size %d, want %d", len(iosPub), crypto.PublicKeySize)
	}
	shared, err := crypto.SharedSecret(partial.CLIPriv, iosPub)
	if err != nil {
		return nil, fmt.Errorf("pairing: shared secret: %w", err)
	}
	transcript := crypto.BuildTranscript(partial.PairID, iosPub, partial.CLIPub)
	session, err := crypto.DeriveSessionKey(shared, transcript, "healthbridge/m2/session")
	if err != nil {
		return nil, fmt.Errorf("pairing: derive session: %w", err)
	}
	out := *partial
	out.IOSPub = iosPub
	out.SessionKey = session
	out.AuthToken = *state.AuthToken
	out.SAS = crypto.SAS(shared, transcript)
	return &out, nil
}

// RespondPairing runs the iOS (responder) half of the protocol given a
// PairLink decoded from the QR the CLI showed. It generates an X25519
// keypair, commits the pubkey to the relay, derives the session key,
// and returns a Result the caller should display (SAS) and persist.
//
// This helper exists in Go so end-to-end tests can drive the iOS side
// from the same process as the CLI side. The production iOS app has its
// own Swift mirror in HealthBridgeKit/Pairing.swift.
func RespondPairing(ctx context.Context, c *relay.Client, link *PairLink) (*Result, error) {
	if c.PairID != link.PairID {
		return nil, fmt.Errorf("pairing: client pair_id %q != link pair_id %q", c.PairID, link.PairID)
	}
	cliPub, err := hex.DecodeString(link.CLIPub)
	if err != nil {
		return nil, fmt.Errorf("pairing: bad cli_pub: %w", err)
	}
	if len(cliPub) != crypto.PublicKeySize {
		return nil, fmt.Errorf("pairing: cli_pub size %d, want %d", len(cliPub), crypto.PublicKeySize)
	}
	keys, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("pairing: gen ios keypair: %w", err)
	}
	state, err := c.PostPubkey(ctx, "ios", hex.EncodeToString(keys.Public))
	if err != nil {
		return nil, fmt.Errorf("pairing: post ios pubkey: %w", err)
	}
	if state.AuthToken == nil {
		return nil, errors.New("pairing: relay did not issue auth_token (cli side missing?)")
	}
	shared, err := crypto.SharedSecret(keys.Private, cliPub)
	if err != nil {
		return nil, err
	}
	transcript := crypto.BuildTranscript(link.PairID, keys.Public, cliPub)
	session, err := crypto.DeriveSessionKey(shared, transcript, "healthbridge/m2/session")
	if err != nil {
		return nil, err
	}
	return &Result{
		PairID:     link.PairID,
		RelayURL:   link.RelayURL,
		SessionKey: session,
		AuthToken:  *state.AuthToken,
		IOSPub:     keys.Public,
		IOSPriv:    keys.Private,
		CLIPub:     cliPub,
		SAS:        crypto.SAS(shared, transcript),
	}, nil
}
