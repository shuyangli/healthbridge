package crypto

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestKeyPairsAreUnique(t *testing.T) {
	a, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a.Private, b.Private) {
		t.Error("two key pairs share a private key")
	}
	if bytes.Equal(a.Public, b.Public) {
		t.Error("two key pairs share a public key")
	}
	if len(a.Public) != PublicKeySize {
		t.Errorf("public size = %d, want %d", len(a.Public), PublicKeySize)
	}
}

func TestSharedSecretIsSymmetric(t *testing.T) {
	alice, _ := GenerateKeyPair()
	bob, _ := GenerateKeyPair()
	abc, err := SharedSecret(alice.Private, bob.Public)
	if err != nil {
		t.Fatal(err)
	}
	bac, err := SharedSecret(bob.Private, alice.Public)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(abc, bac) {
		t.Error("X25519(a,B) != X25519(b,A) — keys not symmetric")
	}
}

func TestRFC7748TestVector(t *testing.T) {
	// Vector from RFC 7748 §6.1: Alice and Bob's shared secret.
	alicePriv, _ := hex.DecodeString("77076d0a7318a57d3c16c17251b26645df4c2f87ebc0992ab177fba51db92c2a")
	bobPub, _ := hex.DecodeString("de9edb7d7b7dc1b4d35b61c2ece435373f8343c85b78674dadfc7e146f882b4f")
	wantShared, _ := hex.DecodeString("4a5d9d5ba4ce2de1728e3bf480350f25e07e21c947d19e3376f09b3c1e161742")

	got, err := SharedSecret(alicePriv, bobPub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wantShared) {
		t.Errorf("RFC 7748 vector mismatch:\n got %x\nwant %x", got, wantShared)
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, SessionKeySize)
	plaintext := []byte("the iPhone says hello")
	aad := []byte("pair=01J9...|job=abc|kind=read")

	sealed, err := Seal(key, plaintext, aad)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if len(sealed) != NonceSize+len(plaintext)+OverheadSize {
		t.Errorf("sealed len = %d, want %d", len(sealed), NonceSize+len(plaintext)+OverheadSize)
	}

	got, err := Open(key, sealed, aad)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("plaintext mismatch: got %q want %q", got, plaintext)
	}
}

func TestOpenRejectsTampered(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, SessionKeySize)
	sealed, err := Seal(key, []byte("payload"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	// Flip one bit in the ciphertext (after the nonce).
	sealed[NonceSize+1] ^= 0x01
	if _, err := Open(key, sealed, []byte("aad")); err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

func TestOpenRejectsBadAAD(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, SessionKeySize)
	sealed, _ := Seal(key, []byte("payload"), []byte("good-aad"))
	if _, err := Open(key, sealed, []byte("bad-aad")); err == nil {
		t.Error("expected error for AAD mismatch")
	}
}

func TestSealWithFixedNonceIsDeterministic(t *testing.T) {
	key := bytes.Repeat([]byte{0xab}, SessionKeySize)
	nonce := bytes.Repeat([]byte{0x01}, NonceSize)
	a, err := SealWithNonce(key, nonce, []byte("hello"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := SealWithNonce(key, nonce, []byte("hello"), []byte("aad"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("SealWithNonce should be deterministic with fixed nonce")
	}
}

func TestDeriveSessionKeyAgreesAcrossSides(t *testing.T) {
	alice, _ := GenerateKeyPair()
	bob, _ := GenerateKeyPair()
	abc, _ := SharedSecret(alice.Private, bob.Public)
	bac, _ := SharedSecret(bob.Private, alice.Public)

	transcript := BuildTranscript("01J9ZX0PAIR000000000000001", alice.Public, bob.Public)
	keyA, err := DeriveSessionKey(abc, transcript, "healthbridge/m2/session")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := DeriveSessionKey(bac, transcript, "healthbridge/m2/session")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(keyA, keyB) {
		t.Error("session keys diverged across sides")
	}
}

func TestDifferentTranscriptsProduceDifferentKeys(t *testing.T) {
	shared := bytes.Repeat([]byte{0xcc}, SharedKeySize)
	a, _ := DeriveSessionKey(shared, []byte("transcript-1"), "info")
	b, _ := DeriveSessionKey(shared, []byte("transcript-2"), "info")
	if bytes.Equal(a, b) {
		t.Error("transcripts must influence the derived key")
	}
}

func TestSASIsSixDigits(t *testing.T) {
	shared := bytes.Repeat([]byte{0xcc}, SharedKeySize)
	transcript := []byte("transcript")
	got := SAS(shared, transcript)
	if len(got) != 6 {
		t.Errorf("SAS = %q, want 6 digits", got)
	}
	for _, c := range got {
		if c < '0' || c > '9' {
			t.Errorf("SAS contains non-digit: %q", got)
		}
	}
}

func TestSASMatchesAcrossSides(t *testing.T) {
	alice, _ := GenerateKeyPair()
	bob, _ := GenerateKeyPair()
	abc, _ := SharedSecret(alice.Private, bob.Public)
	bac, _ := SharedSecret(bob.Private, alice.Public)
	transcript := BuildTranscript("01J9ZX0PAIR000000000000001", alice.Public, bob.Public)
	if SAS(abc, transcript) != SAS(bac, transcript) {
		t.Error("SAS values diverged across sides")
	}
}

func TestSASChangesOnTamperedTranscript(t *testing.T) {
	alice, _ := GenerateKeyPair()
	bob, _ := GenerateKeyPair()
	mallory, _ := GenerateKeyPair()
	abc, _ := SharedSecret(alice.Private, bob.Public)
	// Alice's view of the transcript is correct (her own pub, bob's pub).
	good := BuildTranscript("pair", alice.Public, bob.Public)
	// But what bob sees on the relay has had his expected pub replaced
	// with mallory's. The shared secret is wrong on his end too, but for
	// this test we just verify that even if both sides somehow shared the
	// same secret (e.g. test-only injection), a different transcript still
	// changes the SAS.
	bad := BuildTranscript("pair", alice.Public, mallory.Public)
	if SAS(abc, good) == SAS(abc, bad) {
		t.Error("SAS should differ when the transcript differs")
	}
}
