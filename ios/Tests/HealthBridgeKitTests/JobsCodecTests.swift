import XCTest
import CryptoKit
@testable import HealthBridgeKit

final class JobsCodecTests: XCTestCase {

    private func newSession(pairID: String = "01J9ZX0PAIR000000000000001") -> JobsSession {
        let key = SymmetricKey(data: Data(repeating: 0xab, count: 32))
        return JobsSession(key: key, pairID: pairID)
    }

    func testReadJobRoundTripsThroughEncryptedBlob() throws {
        let from = ISO8601DateFormatter().date(from: "2026-04-01T00:00:00Z")!
        let to = ISO8601DateFormatter().date(from: "2026-04-07T00:00:00Z")!
        let payload = ReadPayload(type: .stepCount, from: from, to: to, limit: nil)
        let job = Job(
            id: "test-job-id",
            kind: .read,
            createdAt: ISO8601DateFormatter().date(from: "2026-04-07T12:00:00Z")!,
            payload: try AnyCodable.from(payload)
        )

        let session = newSession()
        let blob = try session.sealJob(job)
        XCTAssertFalse(blob.isEmpty)
        // Sealed blob must not leak the field name "step_count" in the clear.
        XCTAssertFalse(blob.contains("step_count"))

        let decoded = try session.openJob(jobID: "test-job-id", blob: blob)
        XCTAssertEqual(decoded.id, "test-job-id")
        XCTAssertEqual(decoded.kind, .read)

        let rp = try decoded.decodeReadPayload()
        XCTAssertEqual(rp.type, .stepCount)
        XCTAssertEqual(rp.from, from)
        XCTAssertEqual(rp.to, to)
    }

    func testOpenJobRejectsWrongJobID() throws {
        let session = newSession()
        let job = Job(id: "real-id", kind: .read, createdAt: Date(), payload: try AnyCodable.from(ReadPayload(type: .stepCount, from: Date(), to: Date().addingTimeInterval(60))))
        let blob = try session.sealJob(job)
        XCTAssertThrowsError(try session.openJob(jobID: "wrong-id", blob: blob))
    }

    func testOpenJobRejectsWrongPair() throws {
        let a = newSession(pairID: "01J9ZX0PAIR000000000000001")
        let b = newSession(pairID: "01J9ZX0PAIR000000000000999")
        let job = Job(id: "id", kind: .read, createdAt: Date(), payload: try AnyCodable.from(ReadPayload(type: .stepCount, from: Date(), to: Date().addingTimeInterval(60))))
        let blob = try a.sealJob(job)
        XCTAssertThrowsError(try b.openJob(jobID: "id", blob: blob))
    }

    func testOpenJobRejectsWrongKey() throws {
        let session = newSession()
        let wrong = JobsSession(key: SymmetricKey(data: Data(repeating: 0xcd, count: 32)), pairID: session.pairID)
        let job = Job(id: "id", kind: .read, createdAt: Date(), payload: try AnyCodable.from(ReadPayload(type: .stepCount, from: Date(), to: Date().addingTimeInterval(60))))
        let blob = try session.sealJob(job)
        XCTAssertThrowsError(try wrong.openJob(jobID: "id", blob: blob))
    }

    func testJobResultRoundTrip() throws {
        let session = newSession()
        let sample = Sample(
            type: .stepCount,
            value: 8421,
            unit: "count",
            start: ISO8601DateFormatter().date(from: "2026-04-06T00:00:00Z")!,
            end: ISO8601DateFormatter().date(from: "2026-04-07T00:00:00Z")!
        )
        let result = JobResult(
            jobID: "abc",
            pageIndex: 0,
            status: .done,
            result: try AnyCodable.from(ReadResult(type: .stepCount, samples: [sample]))
        )
        let blob = try session.sealResult(jobID: "abc", pageIndex: 0, result)
        let decoded = try session.openResult(jobID: "abc", pageIndex: 0, blob: blob)
        XCTAssertEqual(decoded.jobID, "abc")
        XCTAssertEqual(decoded.status, .done)

        let rr = try decoded.result!.decode(ReadResult.self)
        XCTAssertEqual(rr.type, .stepCount)
        XCTAssertEqual(rr.samples.count, 1)
        XCTAssertEqual(rr.samples[0].value, 8421)
    }

    func testJobResultRoundTripPreservesUnitValueOneAsNumber() throws {
        let session = newSession()
        let sample = Sample(
            type: .stepCount,
            value: 1,
            unit: "count",
            start: ISO8601DateFormatter().date(from: "2026-04-06T00:00:00Z")!,
            end: ISO8601DateFormatter().date(from: "2026-04-06T00:01:00Z")!
        )
        let result = JobResult(
            jobID: "one-step",
            pageIndex: 0,
            status: .done,
            result: try AnyCodable.from(ReadResult(type: .stepCount, samples: [sample]))
        )
        let blob = try session.sealResult(jobID: "one-step", pageIndex: 0, result)
        let decoded = try session.openResult(jobID: "one-step", pageIndex: 0, blob: blob)
        let rr = try decoded.result!.decode(ReadResult.self)
        XCTAssertEqual(rr.samples.count, 1)
        XCTAssertEqual(rr.samples[0].value, 1)

        let encoded = try JSONEncoder.iso8601.encode(decoded)
        let str = String(decoding: encoded, as: UTF8.self)
        XCTAssertTrue(str.contains("\"value\":1"), "expected numeric JSON value, got \(str)")
        XCTAssertFalse(str.contains("\"value\":true"), "unexpected boolean JSON value in \(str)")
    }

    func testOpenResultRejectsWrongPageIndex() throws {
        let session = newSession()
        let result = JobResult(jobID: "j", pageIndex: 0, status: .done)
        let blob = try session.sealResult(jobID: "j", pageIndex: 0, result)
        XCTAssertThrowsError(try session.openResult(jobID: "j", pageIndex: 1, blob: blob))
    }

    func testDecodeReadPayloadOnWriteJobThrows() throws {
        let job = Job(
            id: "j",
            kind: .write,
            createdAt: Date(),
            payload: AnyCodable([String: Any]())
        )
        XCTAssertThrowsError(try job.decodeReadPayload()) { err in
            XCTAssertEqual(err as? JobDecodeError, .wrongKind(expected: .read, actual: .write))
        }
    }
}
