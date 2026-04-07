// Swift mirror of cli/internal/crypto/crypto.go.
//
// This file uses Apple's CryptoKit for the underlying primitives:
//
//   - Curve25519.KeyAgreement for X25519
//   - HKDF<SHA256> for derivation
//   - ChaChaPoly for AEAD
//
// The wire format and the transcript layout are identical to the Go side,
// so a blob sealed by either party can be opened by the other.

import Foundation
import CryptoKit

public enum HealthBridgeCrypto {

    public static let publicKeySize = 32
    public static let sharedKeySize = 32
    public static let sessionKeySize = 32
    public static let nonceSize = 12       // ChaCha20-Poly1305
    public static let overheadSize = 16    // Poly1305 tag

    public enum CryptoError: Error, Equatable {
        case keySize
        case nonceSize
        case sealedTooShort
        case openFailed
    }

    public struct KeyPair {
        public let privateKey: Curve25519.KeyAgreement.PrivateKey
        public var publicKeyBytes: Data { Data(privateKey.publicKey.rawRepresentation) }

        public init() {
            self.privateKey = Curve25519.KeyAgreement.PrivateKey()
        }

        public init(rawPrivate: Data) throws {
            self.privateKey = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: rawPrivate)
        }
    }

    /// Compute the X25519 shared secret with the peer's public key.
    public static func sharedSecret(myPriv: Data, theirPub: Data) throws -> Data {
        guard myPriv.count == publicKeySize, theirPub.count == publicKeySize else {
            throw CryptoError.keySize
        }
        let priv = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: myPriv)
        let pub = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: theirPub)
        let secret = try priv.sharedSecretFromKeyAgreement(with: pub)
        // CryptoKit's SharedSecret stores the raw 32 bytes; export them.
        return secret.withUnsafeBytes { Data($0) }
    }

    /// Build the canonical transcript bound by both DeriveSessionKey and SAS.
    /// MUST stay byte-identical with cli/internal/crypto.BuildTranscript.
    public static func buildTranscript(pairID: String, iosPub: Data, cliPub: Data) -> Data {
        var out = Data()
        out.append("healthbridge-pair-v1".data(using: .utf8)!)
        out.append(pairID.data(using: .utf8)!)
        out.append(iosPub)
        out.append(cliPub)
        return out
    }

    /// HKDF-derive a 32-byte session key from the X25519 shared secret +
    /// transcript salt + a per-purpose info string.
    public static func deriveSessionKey(shared: Data, transcript: Data, info: String) -> SymmetricKey {
        let salt = SHA256.hash(data: transcript)
        let symShared = SymmetricKey(data: shared)
        let key = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: symShared,
            salt: Data(salt),
            info: info.data(using: .utf8)!,
            outputByteCount: sessionKeySize
        )
        return key
    }

    /// AEAD seal: returns nonce || ciphertext || tag.
    public static func seal(key: SymmetricKey, plaintext: Data, aad: Data) throws -> Data {
        let sealedBox = try ChaChaPoly.seal(plaintext, using: key, authenticating: aad)
        // ChaChaPoly.SealedBox.combined gives nonce || ct || tag.
        return sealedBox.combined
    }

    /// AEAD seal with a caller-supplied nonce. Used by tests for cross-language
    /// fixtures; production code should let CryptoKit pick the nonce.
    public static func sealWithNonce(key: SymmetricKey, nonce: Data, plaintext: Data, aad: Data) throws -> Data {
        guard nonce.count == nonceSize else { throw CryptoError.nonceSize }
        let n = try ChaChaPoly.Nonce(data: nonce)
        let box = try ChaChaPoly.seal(plaintext, using: key, nonce: n, authenticating: aad)
        return box.combined
    }

    /// AEAD open: takes nonce || ciphertext || tag and returns the plaintext.
    public static func open(key: SymmetricKey, sealed: Data, aad: Data) throws -> Data {
        guard sealed.count >= nonceSize + overheadSize else { throw CryptoError.sealedTooShort }
        do {
            let box = try ChaChaPoly.SealedBox(combined: sealed)
            return try ChaChaPoly.open(box, using: key, authenticating: aad)
        } catch {
            throw CryptoError.openFailed
        }
    }

    /// Compute a 6-digit short-authentication-string. Must match the Go side.
    public static func sas(shared: Data, transcript: Data) -> String {
        let symShared = SymmetricKey(data: shared)
        let key = HKDF<SHA256>.deriveKey(
            inputKeyMaterial: symShared,
            salt: transcript,
            info: "healthbridge/m2/sas".data(using: .utf8)!,
            outputByteCount: 4
        )
        let bytes = key.withUnsafeBytes { Array($0) }
        let n = (UInt32(bytes[0]) << 24)
            | (UInt32(bytes[1]) << 16)
            | (UInt32(bytes[2]) << 8)
            |  UInt32(bytes[3])
        return String(format: "%06d", n % 1_000_000)
    }
}
