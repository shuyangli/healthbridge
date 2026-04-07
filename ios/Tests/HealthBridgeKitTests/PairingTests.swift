import XCTest
import CryptoKit
@testable import HealthBridgeKit

/// Tests for the iOS responder side of the pairing protocol. The Go side
/// (`cli/internal/pairing`) is the source of truth; these tests assert
/// that the Swift mirror produces the same session key + SAS for the same
/// inputs and that error paths surface as typed Swift errors the UI can
/// match on.
final class PairingTests: XCTestCase {

    // MARK: - decodeLink

    func testDecodeLinkRoundTripsCanonicalShape() throws {
        let json = """
        {"v":"healthbridge-pair-v2","pair_id":"01J9ZX0PAIR000000000000001","cli_pub_hex":"\(String(repeating: "ab", count: 32))","relay_url":"https://example.com"}
        """
        let link = try HealthBridgePairing.decodeLink(json)
        XCTAssertEqual(link.pairID, "01J9ZX0PAIR000000000000001")
        XCTAssertEqual(link.cliPubHex, String(repeating: "ab", count: 32))
        XCTAssertEqual(link.relayURL, "https://example.com")
        XCTAssertEqual(link.version, HealthBridgePairing.protocolVersion)
    }

    func testDecodeLinkRejectsV1Shape() {
        // The pre-flip protocol used `ios_pub_hex` and "healthbridge-pair-v1".
        // Old QRs lying around must fail loudly rather than silently
        // produce the wrong session key.
        let v1 = """
        {"v":"healthbridge-pair-v1","pair_id":"x","ios_pub_hex":"deadbeef","relay_url":"https://example.com"}
        """
        XCTAssertThrowsError(try HealthBridgePairing.decodeLink(v1)) { err in
            guard case PairingError.unsupportedProtocolVersion(let v) = err else {
                XCTFail("expected unsupportedProtocolVersion, got \(err)"); return
            }
            XCTAssertEqual(v, "healthbridge-pair-v1")
        }
    }

    func testDecodeLinkRejectsMissingFields() {
        let bad = """
        {"v":"healthbridge-pair-v2","pair_id":"x","cli_pub_hex":""}
        """
        XCTAssertThrowsError(try HealthBridgePairing.decodeLink(bad))
    }

    func testDecodeLinkRejectsBadHex() {
        let bad = """
        {"v":"healthbridge-pair-v2","pair_id":"x","cli_pub_hex":"not-hex","relay_url":"https://example.com"}
        """
        XCTAssertThrowsError(try HealthBridgePairing.decodeLink(bad)) { err in
            XCTAssertEqual(err as? PairingError, .badCLIPubHex)
        }
    }

    func testDecodeLinkRejectsWrongPubkeySize() {
        // Valid hex but only 16 bytes — X25519 needs 32.
        let bad = """
        {"v":"healthbridge-pair-v2","pair_id":"x","cli_pub_hex":"\(String(repeating: "00", count: 16))","relay_url":"https://example.com"}
        """
        XCTAssertThrowsError(try HealthBridgePairing.decodeLink(bad)) { err in
            XCTAssertEqual(err as? PairingError, .wrongPubkeySize)
        }
    }

    // MARK: - respondPairing

    /// Drives respondPairing against a mocked relay. The mock plays the
    /// CLI side: it has a pre-baked CLI keypair (so we can verify the
    /// derived session matches), accepts the iOS POST, and returns the
    /// expected PairState with an auth token.
    func testRespondPairingDerivesSameSessionAsGoTranscript() async throws {
        // Use the cross-language fixtures so the derived session key is
        // bit-for-bit checkable against the Go side.
        let cliPriv = CrossLanguageCryptoTests.cliPriv
        let cliPub = CrossLanguageCryptoTests.cliPub
        let pairID = CrossLanguageCryptoTests.pairID
        let expectedAuth = "test-auth-token"

        var seenIOSPubHex: String?
        MockURLProtocol.handler = { req in
            XCTAssertEqual(req.httpMethod, "POST")
            XCTAssertTrue(req.url!.absoluteString.contains("/v1/pair"))
            let body = try JSONSerialization.jsonObject(with: req.bodyStreamData() ?? Data()) as! [String: Any]
            XCTAssertEqual(body["side"] as? String, "ios")
            seenIOSPubHex = body["pubkey"] as? String

            let payload: [String: Any] = [
                "ios_pub": seenIOSPubHex!,
                "cli_pub": cliPub.hexString,
                "auth_token": expectedAuth,
                "completed_at": 12345,
            ]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: req.url!, statusCode: 201, httpVersion: nil, headerFields: nil)!, data)
        }
        defer { MockURLProtocol.handler = nil }

        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [MockURLProtocol.self]
        let session = URLSession(configuration: cfg)
        let client = RelayClient(
            baseURL: URL(string: "https://relay.example.com")!,
            pairID: pairID,
            session: session
        )
        let link = PairLink(
            pairID: pairID,
            cliPubHex: cliPub.hexString,
            relayURL: "https://relay.example.com"
        )
        let result = try await HealthBridgePairing.respondPairing(client: client, link: link)

        // The CLI side derives the session from cliPriv + iosPub. We have
        // both: cliPriv from the fixture, iosPub from the request the
        // mock captured. Run the same Go-mirroring derivation and check
        // the SessionKey matches.
        let iosPub = Data(hexString: seenIOSPubHex!)!
        let shared = try HealthBridgeCrypto.sharedSecret(myPriv: cliPriv, theirPub: iosPub)
        let transcript = HealthBridgeCrypto.buildTranscript(pairID: pairID, iosPub: iosPub, cliPub: cliPub)
        let cliSession = HealthBridgeCrypto.deriveSessionKey(
            shared: shared,
            transcript: transcript,
            info: "healthbridge/m2/session"
        )
        let cliSessionData = cliSession.withUnsafeBytes { Data($0) }
        let cliSAS = HealthBridgeCrypto.sas(shared: shared, transcript: transcript)

        XCTAssertEqual(result.sessionKey, cliSessionData)
        XCTAssertEqual(result.sas, cliSAS)
        XCTAssertEqual(result.authToken, expectedAuth)
        XCTAssertEqual(result.pairID, pairID)
        XCTAssertEqual(result.cliPub, cliPub)
        XCTAssertEqual(result.iosPub, iosPub)
    }

    func testRespondPairingThrowsWhenRelayMissesAuthToken() async throws {
        MockURLProtocol.handler = { req in
            let payload: [String: Any] = [
                "ios_pub": "00",
                "cli_pub": "00",
                "auth_token": NSNull(),
                "completed_at": NSNull(),
            ]
            let data = try JSONSerialization.data(withJSONObject: payload)
            return (HTTPURLResponse(url: req.url!, statusCode: 201, httpVersion: nil, headerFields: nil)!, data)
        }
        defer { MockURLProtocol.handler = nil }

        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [MockURLProtocol.self]
        let session = URLSession(configuration: cfg)
        let client = RelayClient(
            baseURL: URL(string: "https://relay.example.com")!,
            pairID: "01J9ZX0PAIR000000000000001",
            session: session
        )
        let link = PairLink(
            pairID: "01J9ZX0PAIR000000000000001",
            cliPubHex: CrossLanguageCryptoTests.cliPub.hexString,
            relayURL: "https://relay.example.com"
        )
        do {
            _ = try await HealthBridgePairing.respondPairing(client: client, link: link)
            XCTFail("expected throw")
        } catch PairingError.relayMissingAuthToken {
            // expected
        } catch {
            XCTFail("unexpected error: \(error)")
        }
    }
}
