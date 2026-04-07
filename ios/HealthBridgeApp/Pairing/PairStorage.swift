// Persistent storage for the iOS side of a HealthBridge pair.
//
// V1 stores a single pair record as a JSON file under the app's
// Application Support directory with file-protection set to
// `.completeUntilFirstUserAuthentication`. The session key never leaves
// this file in plaintext form except in memory while the app is running.
//
// Trade-offs vs Keychain:
//   - Keychain `kSecAttrAccessibleWhenUnlockedThisDeviceOnly` is the
//     "right" answer and what M3 of the design doc calls for. We will
//     migrate when the consent ledger lands; for now we want to ship the
//     pairing UX without entangling Keychain semantics + provisioning.
//   - File-protection class is enforced by the OS regardless of the
//     storage backend, so the file is unreadable until the user has
//     unlocked the device once after boot.
//
// Multi-pair is intentionally out of scope: V1 binds the iPhone to one
// CLI at a time. The pair record is overwritten on a new pair and
// removed by `PairStorage.clear()`.

#if os(iOS)
import Foundation
import HealthBridgeKit

/// Stored form of a successful pairing. Mirrors the fields of
/// `PairResult` but written via Codable so the on-disk format is stable.
struct StoredPair: Codable, Equatable {
    var pairID: String
    var relayURL: String
    var sessionKeyHex: String
    var authToken: String
    var iosPubHex: String
    var cliPubHex: String
    var sas: String
    var pairedAt: Date

    enum CodingKeys: String, CodingKey {
        case pairID = "pair_id"
        case relayURL = "relay_url"
        case sessionKeyHex = "session_key_hex"
        case authToken = "auth_token"
        case iosPubHex = "ios_pub_hex"
        case cliPubHex = "cli_pub_hex"
        case sas
        case pairedAt = "paired_at"
    }

    init(from result: PairResult, pairedAt: Date = Date()) {
        self.pairID = result.pairID
        self.relayURL = result.relayURL
        self.sessionKeyHex = result.sessionKey.hexString
        self.authToken = result.authToken
        self.iosPubHex = result.iosPub.hexString
        self.cliPubHex = result.cliPub.hexString
        self.sas = result.sas
        self.pairedAt = pairedAt
    }
}

enum PairStorageError: Error {
    case fileWrite(Error)
    case fileRead(Error)
    case decode(Error)
    case noApplicationSupport
}

enum PairStorage {

    private static let filename = "pair.json"

    private static func storageURL() throws -> URL {
        let fm = FileManager.default
        guard let dir = try? fm.url(
            for: .applicationSupportDirectory,
            in: .userDomainMask,
            appropriateFor: nil,
            create: true
        ) else {
            throw PairStorageError.noApplicationSupport
        }
        // Tag with the bundle identifier so a future user-data backup
        // restore doesn't merge with another app's directory.
        let bundleDir = dir.appendingPathComponent("HealthBridge", isDirectory: true)
        if !fm.fileExists(atPath: bundleDir.path) {
            try fm.createDirectory(at: bundleDir, withIntermediateDirectories: true)
        }
        return bundleDir.appendingPathComponent(filename)
    }

    /// Persist a successful pair, overwriting any previous one.
    static func save(_ pair: StoredPair) throws {
        let url = try storageURL()
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        do {
            let data = try encoder.encode(pair)
            try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
        } catch {
            throw PairStorageError.fileWrite(error)
        }
    }

    /// Load the persisted pair, or nil if no pairing has happened yet.
    static func load() -> StoredPair? {
        let url: URL
        do {
            url = try storageURL()
        } catch {
            return nil
        }
        let data: Data
        do {
            data = try Data(contentsOf: url)
        } catch {
            return nil
        }
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return try? decoder.decode(StoredPair.self, from: data)
    }

    /// Remove the persisted pair (e.g. user revokes from settings).
    static func clear() throws {
        let url = try storageURL()
        if FileManager.default.fileExists(atPath: url.path) {
            try FileManager.default.removeItem(at: url)
        }
    }
}

#endif
