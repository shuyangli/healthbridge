import XCTest
@testable import HealthBridgeKit

final class JobsCodecTests: XCTestCase {

    func testReadJobRoundTripsThroughBase64() throws {
        let from = ISO8601DateFormatter().date(from: "2026-04-01T00:00:00Z")!
        let to = ISO8601DateFormatter().date(from: "2026-04-07T00:00:00Z")!
        let payload = ReadPayload(type: .stepCount, from: from, to: to, limit: nil)

        let job = Job(
            id: "test-job-id",
            kind: .read,
            createdAt: ISO8601DateFormatter().date(from: "2026-04-07T12:00:00Z")!,
            payload: try AnyCodable.from(payload)
        )

        let blob = try JobsCodec.encode(job)
        XCTAssertFalse(blob.isEmpty)

        let decoded = try JobsCodec.decodeJob(blob)
        XCTAssertEqual(decoded.id, "test-job-id")
        XCTAssertEqual(decoded.kind, .read)

        let rp = try decoded.decodeReadPayload()
        XCTAssertEqual(rp.type, .stepCount)
        XCTAssertEqual(rp.from, from)
        XCTAssertEqual(rp.to, to)
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

    func testJobResultRoundTrip() throws {
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

        let blob = try JobsCodec.encode(result)
        let decoded = try JobsCodec.decodeResult(blob)
        XCTAssertEqual(decoded.jobID, "abc")
        XCTAssertEqual(decoded.status, .done)

        let rr = try decoded.result!.decode(ReadResult.self)
        XCTAssertEqual(rr.type, .stepCount)
        XCTAssertEqual(rr.samples.count, 1)
        XCTAssertEqual(rr.samples[0].value, 8421)
    }

    func testDecodeJobRejectsInvalidBase64() {
        XCTAssertThrowsError(try JobsCodec.decodeJob("not!base64"))
    }
}
