// AnyCodable is a tiny utility for round-tripping arbitrary JSON values
// inside a typed Codable struct. We use it for the Job.payload field where
// the static type depends on the runtime job kind.
//
// This is a minimal hand-rolled implementation, no third-party dependency.

import Foundation

/// AnyCodable holds an arbitrary JSON-shaped value (`String`, `Double`,
/// `Int`, `Bool`, `[Any]`, `[String: Any]`, `NSNull`). The wrapper is
/// intentionally `@unchecked Sendable`: its only mutable surface is `value`,
/// which we treat as immutable by convention since the type is `let`.
public struct AnyCodable: Codable, @unchecked Sendable {
    public let value: Any

    public init(_ value: Any) {
        self.value = value
    }

    /// Encode an arbitrary `Encodable` value into an AnyCodable. Used by
    /// callers that want to wrap a typed payload like `ReadResult` for
    /// transport in a `JobResult.result`.
    public static func from<T: Encodable>(_ value: T) throws -> AnyCodable {
        let data = try JSONEncoder.iso8601.encode(value)
        let json = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed])
        return AnyCodable(json)
    }

    /// Decode this AnyCodable into a concrete type. The reverse of `from`.
    public func decode<T: Decodable>(_ type: T.Type) throws -> T {
        let data = try JSONSerialization.data(withJSONObject: value, options: [.fragmentsAllowed])
        return try JSONDecoder.iso8601.decode(type, from: data)
    }

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self.value = NSNull()
        } else if let v = try? container.decode(Bool.self) {
            self.value = v
        } else if let v = try? container.decode(Int.self) {
            self.value = v
        } else if let v = try? container.decode(Double.self) {
            self.value = v
        } else if let v = try? container.decode(String.self) {
            self.value = v
        } else if let v = try? container.decode([AnyCodable].self) {
            self.value = v.map(\.value)
        } else if let v = try? container.decode([String: AnyCodable].self) {
            self.value = v.mapValues(\.value)
        } else {
            throw DecodingError.dataCorruptedError(
                in: container,
                debugDescription: "AnyCodable cannot decode value"
            )
        }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch value {
        case is NSNull:
            try container.encodeNil()
        case let v as Bool:
            try container.encode(v)
        case let v as Int:
            try container.encode(v)
        case let v as Int64:
            try container.encode(v)
        case let v as Double:
            try container.encode(v)
        case let v as String:
            try container.encode(v)
        case let v as [Any]:
            try container.encode(v.map { AnyCodable($0) })
        case let v as [String: Any]:
            try container.encode(v.mapValues { AnyCodable($0) })
        default:
            // Last-ditch: re-encode via JSONSerialization. This handles NSNumber
            // boxed types from JSONSerialization output.
            if JSONSerialization.isValidJSONObject([value]) {
                let data = try JSONSerialization.data(withJSONObject: [value], options: [])
                let arr = try JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]) as? [Any] ?? []
                if let first = arr.first {
                    try container.encode(AnyCodable(first))
                    return
                }
            }
            throw EncodingError.invalidValue(value, EncodingError.Context(
                codingPath: container.codingPath,
                debugDescription: "AnyCodable cannot encode \(type(of: value))"
            ))
        }
    }
}

/// JSONEncoder/Decoder presets used throughout the kit. ISO8601 with
/// fractional seconds matches what the Go side emits via `time.Time`.
public extension JSONEncoder {
    static let iso8601: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()
}

public extension JSONDecoder {
    static let iso8601: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()
}
