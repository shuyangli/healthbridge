// Codec that wraps and unwraps the relay's blob format. In M1 it is
// base64(JSON(job)); M2 will replace the inner step with XChaCha20-Poly1305
// ciphertext but the public API of this file stays the same.

import Foundation

public enum JobsCodec {
    public static func encode(_ job: Job) throws -> String {
        let data = try JSONEncoder.iso8601.encode(job)
        return data.base64EncodedString()
    }

    public static func decodeJob(_ blob: String) throws -> Job {
        guard let data = Data(base64Encoded: blob) else {
            throw CodecError.invalidBase64
        }
        return try JSONDecoder.iso8601.decode(Job.self, from: data)
    }

    public static func encode(_ result: JobResult) throws -> String {
        let data = try JSONEncoder.iso8601.encode(result)
        return data.base64EncodedString()
    }

    public static func decodeResult(_ blob: String) throws -> JobResult {
        guard let data = Data(base64Encoded: blob) else {
            throw CodecError.invalidBase64
        }
        return try JSONDecoder.iso8601.decode(JobResult.self, from: data)
    }
}

public enum CodecError: Error, Equatable {
    case invalidBase64
}
