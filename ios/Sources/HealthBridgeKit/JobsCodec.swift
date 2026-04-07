// Codec that wraps and unwraps the relay's blob format. As of M2 every
// blob is encrypted with the per-pair session key established at pairing
// time. AAD binds (pair_id, job_id[, page_index]) so the relay cannot
// replay a valid blob across pairs or jobs.
//
// Wire format inside a blob:
//     base64( nonce(12) || ChaCha20-Poly1305(json, key, AAD) || tag(16) )
//
// All callers go through a Session, which carries the key + pair_id.
// This file is the byte-for-byte counterpart of cli/internal/jobs/codec.go.

import Foundation
import CryptoKit

public struct JobsSession: Sendable {
    public let key: SymmetricKey
    public let pairID: String

    public init(key: SymmetricKey, pairID: String) {
        self.key = key
        self.pairID = pairID
    }

    public func sealJob(_ job: Job) throws -> String {
        let plaintext = try JSONEncoder.iso8601.encode(job)
        let aad = JobsCodec.jobAAD(pairID: pairID, jobID: job.id)
        let sealed = try HealthBridgeCrypto.seal(key: key, plaintext: plaintext, aad: aad)
        return sealed.base64EncodedString()
    }

    public func openJob(jobID: String, blob: String) throws -> Job {
        guard let sealed = Data(base64Encoded: blob) else { throw CodecError.invalidBase64 }
        let aad = JobsCodec.jobAAD(pairID: pairID, jobID: jobID)
        let plaintext = try HealthBridgeCrypto.open(key: key, sealed: sealed, aad: aad)
        let job = try JSONDecoder.iso8601.decode(Job.self, from: plaintext)
        guard job.id == jobID else { throw CodecError.envelopeBodyMismatch }
        return job
    }

    public func sealResult(jobID: String, pageIndex: Int, _ result: JobResult) throws -> String {
        var copy = result
        copy.jobID = jobID
        copy.pageIndex = pageIndex
        let plaintext = try JSONEncoder.iso8601.encode(copy)
        let aad = JobsCodec.resultAAD(pairID: pairID, jobID: jobID, pageIndex: pageIndex)
        let sealed = try HealthBridgeCrypto.seal(key: key, plaintext: plaintext, aad: aad)
        return sealed.base64EncodedString()
    }

    public func openResult(jobID: String, pageIndex: Int, blob: String) throws -> JobResult {
        guard let sealed = Data(base64Encoded: blob) else { throw CodecError.invalidBase64 }
        let aad = JobsCodec.resultAAD(pairID: pairID, jobID: jobID, pageIndex: pageIndex)
        let plaintext = try HealthBridgeCrypto.open(key: key, sealed: sealed, aad: aad)
        let result = try JSONDecoder.iso8601.decode(JobResult.self, from: plaintext)
        guard result.jobID == jobID, result.pageIndex == pageIndex else {
            throw CodecError.envelopeBodyMismatch
        }
        return result
    }
}

public enum JobsCodec {
    /// AAD layout MUST stay byte-identical with cli/internal/jobs/codec.go.
    static func jobAAD(pairID: String, jobID: String) -> Data {
        return "pair=\(pairID)|job=\(jobID)".data(using: .utf8)!
    }

    static func resultAAD(pairID: String, jobID: String, pageIndex: Int) -> Data {
        return "pair=\(pairID)|job=\(jobID)|page=\(pageIndex)".data(using: .utf8)!
    }
}

public enum CodecError: Error, Equatable {
    case invalidBase64
    case envelopeBodyMismatch
}
