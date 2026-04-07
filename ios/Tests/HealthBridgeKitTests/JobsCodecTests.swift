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

    func testSleepAnalysisSampleRoundTrip() throws {
        // sleep_analysis reads emit one Sample per HKCategorySample, with
        // value=duration_seconds, unit="s", and the snake_case state in
        // metadata["state"]. Verify the wire format round-trips through
        // an encrypted result blob and survives a typed re-decode.
        let session = newSession()
        let start = ISO8601DateFormatter().date(from: "2026-04-06T23:00:00Z")!
        let end = ISO8601DateFormatter().date(from: "2026-04-07T07:30:00Z")!
        let sample = Sample(
            uuid: "11111111-1111-1111-1111-111111111111",
            type: .sleepAnalysis,
            value: end.timeIntervalSince(start),
            unit: "s",
            start: start,
            end: end,
            metadata: ["state": AnyCodable("asleep_deep")]
        )
        let result = JobResult(
            jobID: "sleep-1",
            status: .done,
            result: try AnyCodable.from(ReadResult(type: .sleepAnalysis, samples: [sample]))
        )
        let blob = try session.sealResult(jobID: "sleep-1", pageIndex: 0, result)
        let decoded = try session.openResult(jobID: "sleep-1", pageIndex: 0, blob: blob)
        let rr = try decoded.result!.decode(ReadResult.self)
        XCTAssertEqual(rr.type, .sleepAnalysis)
        XCTAssertEqual(rr.samples.count, 1)
        XCTAssertEqual(rr.samples[0].unit, "s")
        XCTAssertEqual(rr.samples[0].value, end.timeIntervalSince(start))
        XCTAssertEqual(rr.samples[0].metadata?["state"]?.value as? String, "asleep_deep")
    }

    func testWorkoutSampleRoundTrip() throws {
        // workout reads emit one Sample per HKWorkout, with value=duration,
        // unit="s", and metadata carrying activity_type plus optional
        // total_energy_burned_kcal / total_distance_m numerics. Verify
        // mixed string+number metadata survives the round trip without
        // being mangled by AnyCodable's bool-bridging trap.
        let session = newSession()
        let start = ISO8601DateFormatter().date(from: "2026-04-07T08:00:00Z")!
        let end = ISO8601DateFormatter().date(from: "2026-04-07T08:32:00Z")!
        let sample = Sample(
            uuid: "22222222-2222-2222-2222-222222222222",
            type: .workout,
            value: end.timeIntervalSince(start),
            unit: "s",
            start: start,
            end: end,
            metadata: [
                "activity_type": AnyCodable("running"),
                "total_energy_burned_kcal": AnyCodable(312.5),
                "total_distance_m": AnyCodable(5021.0),
            ]
        )
        let result = JobResult(
            jobID: "workout-1",
            status: .done,
            result: try AnyCodable.from(ReadResult(type: .workout, samples: [sample]))
        )
        let blob = try session.sealResult(jobID: "workout-1", pageIndex: 0, result)
        let decoded = try session.openResult(jobID: "workout-1", pageIndex: 0, blob: blob)
        let rr = try decoded.result!.decode(ReadResult.self)
        XCTAssertEqual(rr.type, .workout)
        XCTAssertEqual(rr.samples.count, 1)
        let meta = rr.samples[0].metadata!
        XCTAssertEqual(meta["activity_type"]?.value as? String, "running")
        XCTAssertEqual(meta["total_energy_burned_kcal"]?.value as? Double, 312.5)
        // Integer-valued doubles must NOT be re-encoded as bool — this
        // is the regression guarded by 36d12f4 — and must come back as
        // a numeric value.
        let dist = meta["total_distance_m"]?.value
        if let d = dist as? Double {
            XCTAssertEqual(d, 5021.0)
        } else if let i = dist as? Int {
            XCTAssertEqual(Double(i), 5021.0)
        } else {
            XCTFail("total_distance_m did not survive as a number; got \(String(describing: dist))")
        }
    }

    func testFailedJobResultRoundTrip() throws {
        // The iOS drain loop converts per-job execution failures (e.g.
        // unsupported sample type) into a JobResult with status=.failed
        // and an error payload, then seals and posts it so the loop can
        // proceed to the next job. Verify that shape survives a
        // seal/open round trip with the error intact.
        let session = newSession()
        let result = JobResult(
            jobID: "bad-job",
            pageIndex: 0,
            status: .failed,
            error: JobError(code: "execute_failed", message: "unsupported sample type step_count")
        )
        let blob = try session.sealResult(jobID: "bad-job", pageIndex: 0, result)
        let decoded = try session.openResult(jobID: "bad-job", pageIndex: 0, blob: blob)
        XCTAssertEqual(decoded.jobID, "bad-job")
        XCTAssertEqual(decoded.status, .failed)
        XCTAssertNil(decoded.result)
        XCTAssertEqual(decoded.error?.code, "execute_failed")
        XCTAssertEqual(decoded.error?.message, "unsupported sample type step_count")
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
