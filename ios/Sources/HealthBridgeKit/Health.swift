// Wire-format types — the Swift mirror of cli/internal/health/types.go and
// proto/schema.json. Updates here must stay in lockstep with the Go side
// or the relay round-trip will break.

import Foundation

/// Stable identifier for a HealthKit sample type. Backed by a wire
/// string (e.g. "step_count") that the Go CLI emits and the iOS app
/// maps to an HKQuantityTypeIdentifier (or HKCategoryType /
/// HKWorkoutType for the non-quantity carryover).
///
/// Conceptually this is an enum with ~120 cases; structurally it is a
/// `RawRepresentable` struct so the catalog can grow without each new
/// HKQuantityTypeIdentifier requiring an enum case here. The
/// individual constants (`SampleType.stepCount`, …) live in
/// `Generated/SampleTypeCatalog.swift` and are regenerated from the
/// Go-side catalog by `cli/cmd/gen-types`.
public struct SampleType: RawRepresentable, Hashable, Sendable {
    public let rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }
}

extension SampleType: Codable {
    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        self.rawValue = try container.decode(String.self)
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }
}

public enum JobKind: String, Codable, Sendable {
    case read, write, sync
}

public enum ResultStatus: String, Codable, Sendable {
    case done, failed
}

public struct Source: Codable, Sendable, Equatable {
    public var name: String?
    public var bundleID: String?

    enum CodingKeys: String, CodingKey {
        case name
        case bundleID = "bundle_id"
    }

    public init(name: String? = nil, bundleID: String? = nil) {
        self.name = name
        self.bundleID = bundleID
    }
}

public struct Sample: Codable, Sendable {
    public var uuid: String?
    public var type: SampleType
    public var value: Double
    public var unit: String
    public var start: Date
    public var end: Date
    public var metadata: [String: AnyCodable]?
    public var source: Source?

    public init(
        uuid: String? = nil,
        type: SampleType,
        value: Double,
        unit: String,
        start: Date,
        end: Date,
        metadata: [String: AnyCodable]? = nil,
        source: Source? = nil
    ) {
        self.uuid = uuid
        self.type = type
        self.value = value
        self.unit = unit
        self.start = start
        self.end = end
        self.metadata = metadata
        self.source = source
    }
}

public struct ReadPayload: Codable, Sendable {
    public var type: SampleType
    public var from: Date
    public var to: Date
    public var limit: Int?

    public init(type: SampleType, from: Date, to: Date, limit: Int? = nil) {
        self.type = type
        self.from = from
        self.to = to
        self.limit = limit
    }
}

public struct ReadResult: Codable, Sendable {
    public var type: SampleType
    public var samples: [Sample]

    public init(type: SampleType, samples: [Sample]) {
        self.type = type
        self.samples = samples
    }
}

public struct WritePayload: Codable, Sendable {
    public var sample: Sample
    public init(sample: Sample) { self.sample = sample }
}

public struct WriteResult: Codable, Sendable, Equatable {
    public var uuid: String
    public init(uuid: String) { self.uuid = uuid }
}

/// Job is the plaintext envelope an agent submits. The payload is one of
/// ReadPayload / WritePayload / SyncPayload (M4) and is decoded based on
/// the job's `kind`. We use AnyCodable so the kit doesn't need to know
/// every payload concretely.
public struct Job: Codable, Sendable {
    public var id: String
    public var kind: JobKind
    public var createdAt: Date
    public var deadline: Date?
    public var payload: AnyCodable

    enum CodingKeys: String, CodingKey {
        case id, kind, payload
        case createdAt = "created_at"
        case deadline
    }

    public init(id: String, kind: JobKind, createdAt: Date, deadline: Date? = nil, payload: AnyCodable) {
        self.id = id
        self.kind = kind
        self.createdAt = createdAt
        self.deadline = deadline
        self.payload = payload
    }

    /// Decode the typed payload for a `read` job. Throws if the kind is wrong.
    public func decodeReadPayload() throws -> ReadPayload {
        guard kind == .read else {
            throw JobDecodeError.wrongKind(expected: .read, actual: kind)
        }
        return try payload.decode(ReadPayload.self)
    }

    /// Decode the typed payload for a `write` job.
    public func decodeWritePayload() throws -> WritePayload {
        guard kind == .write else {
            throw JobDecodeError.wrongKind(expected: .write, actual: kind)
        }
        return try payload.decode(WritePayload.self)
    }
}

public enum JobDecodeError: Error, Equatable {
    case wrongKind(expected: JobKind, actual: JobKind)
}

public struct JobError: Codable, Sendable, Equatable {
    public var code: String
    public var message: String
    public init(code: String, message: String) {
        self.code = code
        self.message = message
    }
}

public struct JobResult: Codable, Sendable {
    public var jobID: String
    public var pageIndex: Int
    public var status: ResultStatus
    public var result: AnyCodable?
    public var error: JobError?

    enum CodingKeys: String, CodingKey {
        case jobID = "job_id"
        case pageIndex = "page_index"
        case status, result, error
    }

    public init(
        jobID: String,
        pageIndex: Int = 0,
        status: ResultStatus,
        result: AnyCodable? = nil,
        error: JobError? = nil
    ) {
        self.jobID = jobID
        self.pageIndex = pageIndex
        self.status = status
        self.result = result
        self.error = error
    }
}
