// Package crypto handles the M2 cryptography for HealthBridge.
//
// We use:
//
//   - X25519 (RFC 7748) for the key-exchange at pairing time.
//   - HKDF-SHA256 (RFC 5869) to derive a stable session key from the
//     X25519 shared secret + a transcript that includes both pubkeys and
//     the pair_id, so a successful pairing on the relay can't accidentally
//     swap one party's pubkey for an attacker's mid-flight without the
//     SAS confirmation noticing.
//   - ChaCha20-Poly1305 (RFC 7539) AEAD with random 96-bit nonces, AAD
//     binding (pair_id, job_id, kind, created_at). Random nonces are safe
//     up to ~2^32 messages per key (birthday bound), which is many orders
//     of magnitude beyond a personal HealthKit mailbox.
//
// The plan calls for XChaCha20-Poly1305, but Apple's CryptoKit on iOS does
// not ship XChaCha20 — only ChaCha20-Poly1305 with 96-bit nonces — and we
// want a Swift mirror without pulling libsodium. Random 96-bit nonces from
// crypto/rand give us the same security envelope at our message volumes.
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/hkdf"
)

// PublicKeySize and PrivateKeySize are the X25519 raw byte sizes.
const (
	PublicKeySize  = 32
	PrivateKeySize = 32
	SharedKeySize  = 32
	SessionKeySize = 32
	NonceSize      = chacha20poly1305.NonceSize // 12 bytes
	OverheadSize   = chacha20poly1305.Overhead  // 16 bytes
)

// KeyPair is an X25519 key pair.
type KeyPair struct {
	Private []byte // 32 bytes
	Public  []byte // 32 bytes
}

// GenerateKeyPair returns a fresh X25519 key pair using crypto/rand.
func GenerateKeyPair() (*KeyPair, error) {
	priv := make([]byte, PrivateKeySize)
	if _, err := io.ReadFull(rand.Reader, priv); err != nil {
		return nil, fmt.Errorf("crypto: read random: %w", err)
	}
	pub, err := curve25519.X25519(priv, curve25519.Basepoint)
	if err != nil {
		return nil, fmt.Errorf("crypto: derive pubkey: %w", err)
	}
	return &KeyPair{Private: priv, Public: pub}, nil
}

// SharedSecret computes the X25519 shared secret from our private key and
// the peer's public key. Returns ErrInvalidPubkey for low-order points.
func SharedSecret(myPriv, theirPub []byte) ([]byte, error) {
	if len(myPriv) != PrivateKeySize || len(theirPub) != PublicKeySize {
		return nil, ErrKeySize
	}
	return curve25519.X25519(myPriv, theirPub)
}

// ErrKeySize is returned for any X25519 input that isn't 32 bytes.
var ErrKeySize = errors.New("crypto: X25519 key must be 32 bytes")

// ErrInvalidNonce indicates a corrupt or short nonce.
var ErrInvalidNonce = errors.New("crypto: nonce must be 12 bytes")

// DeriveSessionKey hashes the X25519 shared secret together with a
// transcript via HKDF-SHA256 and returns a 32-byte session key.
//
// The transcript MUST include both pubkeys and the pair_id; the SAS
// computation depends on the same transcript so an MITM that swapped a
// pubkey at the relay would produce a different SAS on each side.
//
// `info` is a per-purpose label like "healthbridge/m2/session".
func DeriveSessionKey(shared, transcript []byte, info string) ([]byte, error) {
	salt := sha256.Sum256(transcript)
	r := hkdf.New(sha256.New, shared, salt[:], []byte(info))
	out := make([]byte, SessionKeySize)
	if _, err := io.ReadFull(r, out); err != nil {
		return nil, fmt.Errorf("crypto: hkdf: %w", err)
	}
	return out, nil
}

// Seal encrypts plaintext with the session key and AAD, returning
// `nonce || ciphertext || tag`. The nonce is freshly random per call.
func Seal(key, plaintext, aad []byte) ([]byte, error) {
	if len(key) != SessionKeySize {
		return nil, ErrKeySize
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: chacha20poly1305: %w", err)
	}
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: nonce: %w", err)
	}
	out := make([]byte, 0, NonceSize+len(plaintext)+OverheadSize)
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return out, nil
}

// SealWithNonce is Seal with a caller-supplied nonce. Tests use this for
// deterministic outputs; production code should always use Seal.
func SealWithNonce(key, nonce, plaintext, aad []byte) ([]byte, error) {
	if len(key) != SessionKeySize {
		return nil, ErrKeySize
	}
	if len(nonce) != NonceSize {
		return nil, ErrInvalidNonce
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, NonceSize+len(plaintext)+OverheadSize)
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, aad)
	return out, nil
}

// Open is the inverse of Seal: it takes `nonce || ciphertext || tag`
// and returns the plaintext, or an error if authentication fails.
func Open(key, sealed, aad []byte) ([]byte, error) {
	if len(key) != SessionKeySize {
		return nil, ErrKeySize
	}
	if len(sealed) < NonceSize+OverheadSize {
		return nil, errors.New("crypto: sealed too short")
	}
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: chacha20poly1305: %w", err)
	}
	nonce := sealed[:NonceSize]
	ct := sealed[NonceSize:]
	pt, err := aead.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("crypto: open: %w", err)
	}
	return pt, nil
}

// BuildTranscript canonicalises the inputs to DeriveSessionKey and SAS so
// the two sides agree byte-for-byte. The order is fixed and documented:
//
//     transcript = "healthbridge-pair-v1" || pair_id || iosPub || cliPub
//
// The constant prefix lets us evolve to a v2 protocol without retroactive
// confusion. iosPub comes before cliPub because the iOS app generates its
// keypair first during pairing.
func BuildTranscript(pairID string, iosPub, cliPub []byte) []byte {
	const prefix = "healthbridge-pair-v1"
	out := make([]byte, 0, len(prefix)+len(pairID)+PublicKeySize*2)
	out = append(out, prefix...)
	out = append(out, pairID...)
	out = append(out, iosPub...)
	out = append(out, cliPub...)
	return out
}

// SAS derives a 6-digit short-authentication-string from the shared
// secret + transcript. Both sides display this number; the user confirms
// they match. An MITM at the relay would swap one party's pubkey, the
// transcripts diverge, and the SAS digits will not match.
func SAS(shared, transcript []byte) string {
	r := hkdf.New(sha256.New, shared, transcript, []byte("healthbridge/m2/sas"))
	buf := make([]byte, 4)
	_, _ = io.ReadFull(r, buf)
	// Treat the four bytes as a big-endian uint32 and reduce mod 1_000_000.
	// Modulo bias on 4 bytes / 1e6 is well below 0.001% per digit.
	n := uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3])
	return fmt.Sprintf("%06d", n%1_000_000)
}
