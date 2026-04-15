// AuditLog is the iOS app's append-only ledger of every job the drainer
// has executed. The Activity log screen reads it and the user can audit
// "what has the agent done with my Health data". The data shape is
// intentionally narrow — no Health-data values, just metadata about which
// type was touched and the outcome.
//
// Persistence is a JSON file under the app's document directory; we keep
// the on-disk format simple so future versions can grow it without
// migration ceremony. The file is rewritten atomically on every append
// and capped at MAX_ENTRIES to prevent unbounded growth.

import Foundation

public struct AuditEntry: Codable, Sendable, Equatable {
    public enum Kind: String, Codable, Sendable {
        case read
        case write
    }

    public enum Outcome: String, Codable, Sendable {
        case done
        case failed
    }

    public let id: String
    public let jobID: String
    public let kind: Kind
    public let sampleType: String?
    public let outcome: Outcome
    public let errorCode: String?
    public let timestamp: Date

    enum CodingKeys: String, CodingKey {
        case id
        case jobID = "job_id"
        case kind
        case sampleType = "sample_type"
        case outcome
        case errorCode = "error_code"
        case timestamp
    }

    public init(
        id: String = UUID().uuidString,
        jobID: String,
        kind: Kind,
        sampleType: String?,
        outcome: Outcome,
        errorCode: String? = nil,
        timestamp: Date = Date()
    ) {
        self.id = id
        self.jobID = jobID
        self.kind = kind
        self.sampleType = sampleType
        self.outcome = outcome
        self.errorCode = errorCode
        self.timestamp = timestamp
    }
}

public actor AuditLog {
    /// Hard cap on entries kept on disk. Older entries are dropped FIFO.
    public static let maxEntries = 1000

    public let fileURL: URL
    private var entries: [AuditEntry] = []
    private var loaded = false

    public init(fileURL: URL) {
        self.fileURL = fileURL
    }

    public func append(_ entry: AuditEntry) async throws {
        try await ensureLoaded()
        entries.append(entry)
        if entries.count > Self.maxEntries {
            entries.removeFirst(entries.count - Self.maxEntries)
        }
        try persist()
    }

    public func all() async throws -> [AuditEntry] {
        try await ensureLoaded()
        return entries
    }

    public func count() async throws -> Int {
        try await ensureLoaded()
        return entries.count
    }

    public func clear() async throws {
        entries = []
        loaded = true
        try persist()
    }

    private func ensureLoaded() async throws {
        if loaded { return }
        loaded = true
        guard FileManager.default.fileExists(atPath: fileURL.path) else { return }
        let data = try Data(contentsOf: fileURL)
        if data.isEmpty { return }
        entries = try JSONDecoder.iso8601.decode([AuditEntry].self, from: data)
    }

    private func persist() throws {
        try FileManager.default.createDirectory(
            at: fileURL.deletingLastPathComponent(),
            withIntermediateDirectories: true
        )
        let data = try JSONEncoder.iso8601.encode(entries)
        try data.write(to: fileURL, options: [.atomic])
    }
}
