// Swift mirror of cli/internal/pairing/pairing.go for the iOS responder.
//
// Roles:
//   - The CLI is the *initiator*. It mints the pair_id, generates an
//     X25519 keypair, posts the pubkey, and renders the resulting
//     PairLink (`{v, pair_id, cli_pub_hex, relay_url}`) as a QR.
//   - The iOS app is the *responder*. It scans the QR, generates its own
//     X25519 keypair, posts the pubkey, derives the same session key, and
//     shows the SAS for the user to confirm against what the CLI shows.
//
// This file is the iOS half. It is intentionally pure: no UIKit, no
// HealthKit, no Keychain. The pairing UI imports this and persists the
// returned PairResult through whatever storage layer it owns.

import Foundation
import CryptoKit

/// PairLink mirrors `pairing.PairLink` in Go. The CLI prints one of these
/// as a JSON-encoded QR code; the iOS app decodes it after a successful
/// scan and feeds it to `respondPairing`.
public struct PairLink: Codable, Sendable, Equatable {
    public let pairID: String
    public let cliPubHex: String
    public let relayURL: String
    public let version: String

    enum CodingKeys: String, CodingKey {
        case pairID = "pair_id"
        case cliPubHex = "cli_pub_hex"
        case relayURL = "relay_url"
        case version = "v"
    }

    public init(pairID: String, cliPubHex: String, relayURL: String, version: String = HealthBridgePairing.protocolVersion) {
        self.pairID = pairID
        self.cliPubHex = cliPubHex
        self.relayURL = relayURL
        self.version = version
    }
}

/// What the iOS responder gets back from a successful pairing.
public struct PairResult: Sendable, Equatable {
    public let pairID: String
    public let relayURL: String
    public let sessionKey: Data
    public let authToken: String
    public let iosPub: Data
    public let iosPriv: Data
    public let cliPub: Data
    public let sas: String

    public init(
        pairID: String,
        relayURL: String,
        sessionKey: Data,
        authToken: String,
        iosPub: Data,
        iosPriv: Data,
        cliPub: Data,
        sas: String
    ) {
        self.pairID = pairID
        self.relayURL = relayURL
        self.sessionKey = sessionKey
        self.authToken = authToken
        self.iosPub = iosPub
        self.iosPriv = iosPriv
        self.cliPub = cliPub
        self.sas = sas
    }
}

/// Errors raised by the pairing helpers. Each case maps to a clear UI
/// message in the iOS pairing screen.
public enum PairingError: Error, Equatable {
    case invalidLinkJSON
    case unsupportedProtocolVersion(String)
    case missingLinkFields
    case badCLIPubHex
    case wrongPubkeySize
    case relayMissingAuthToken
}

public enum HealthBridgePairing {

    /// Wire-format version. Must match `pairing.protocolVersion` in Go.
    /// v2 is the QR-from-CLI flow; v1 was the (now-removed) QR-from-iOS
    /// flow with `ios_pub_hex` instead of `cli_pub_hex`.
    public static let protocolVersion = "healthbridge-pair-v2"

    /// Decode a PairLink from the JSON string the CLI encoded into the
    /// QR code. Validates protocol version *first* so an old v1 link
    /// (which is missing the v2 field shape entirely) surfaces as
    /// `unsupportedProtocolVersion` rather than `invalidLinkJSON`.
    public static func decodeLink(_ json: String) throws -> PairLink {
        guard let data = json.data(using: .utf8) else {
            throw PairingError.invalidLinkJSON
        }
        // Parse loosely first so the version check runs even if v2-shaped
        // fields are missing.
        guard let raw = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw PairingError.invalidLinkJSON
        }
        let version = raw["v"] as? String ?? ""
        if version != protocolVersion {
            throw PairingError.unsupportedProtocolVersion(version)
        }
        let pairID = raw["pair_id"] as? String ?? ""
        let cliPubHex = raw["cli_pub_hex"] as? String ?? ""
        let relayURL = raw["relay_url"] as? String ?? ""
        if pairID.isEmpty || cliPubHex.isEmpty || relayURL.isEmpty {
            throw PairingError.missingLinkFields
        }
        guard let pub = Data(hexString: cliPubHex) else {
            throw PairingError.badCLIPubHex
        }
        if pub.count != HealthBridgeCrypto.publicKeySize {
            throw PairingError.wrongPubkeySize
        }
        return PairLink(pairID: pairID, cliPubHex: cliPubHex, relayURL: relayURL, version: version)
    }

    /// Run the iOS (responder) half of the protocol. Generates a fresh
    /// X25519 keypair, posts the pubkey to the relay, derives the shared
    /// session key, and returns a PairResult.
    ///
    /// The caller is responsible for displaying the SAS, asking the user
    /// to confirm it matches the CLI's, and persisting the result.
    public static func respondPairing(client: RelayClient, link: PairLink) async throws -> PairResult {
        let cliPub = try requireHex(link.cliPubHex)
        if cliPub.count != HealthBridgeCrypto.publicKeySize {
            throw PairingError.wrongPubkeySize
        }
        let keys = HealthBridgeCrypto.KeyPair()
        let iosPub = keys.publicKeyBytes
        let iosPriv = keys.privateKey.rawRepresentation

        let state = try await client.postPubkey(
            side: "ios",
            pubkeyHex: iosPub.hexString
        )
        guard let token = state.authToken, !token.isEmpty else {
            throw PairingError.relayMissingAuthToken
        }

        let shared = try HealthBridgeCrypto.sharedSecret(myPriv: iosPriv, theirPub: cliPub)
        let transcript = HealthBridgeCrypto.buildTranscript(pairID: link.pairID, iosPub: iosPub, cliPub: cliPub)
        let sessionSym = HealthBridgeCrypto.deriveSessionKey(
            shared: shared,
            transcript: transcript,
            info: "healthbridge/m2/session"
        )
        let sessionData = sessionSym.withUnsafeBytes { Data($0) }
        let sas = HealthBridgeCrypto.sas(shared: shared, transcript: transcript)

        return PairResult(
            pairID: link.pairID,
            relayURL: link.relayURL,
            sessionKey: sessionData,
            authToken: token,
            iosPub: iosPub,
            iosPriv: iosPriv,
            cliPub: cliPub,
            sas: sas
        )
    }

    private static func requireHex(_ s: String) throws -> Data {
        guard let d = Data(hexString: s) else {
            throw PairingError.badCLIPubHex
        }
        return d
    }
}
