import XCTest
@testable import HealthBridgeKit

final class AuditLogTests: XCTestCase {

    private func tempLogURL() -> URL {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        return dir.appendingPathComponent("audit.json")
    }

    func testAppendAndPersistRoundTrip() async throws {
        let url = tempLogURL()
        let log = AuditLog(fileURL: url)
        try await log.append(AuditEntry(
            jobID: "job-1",
            kind: .read,
            sampleType: "step_count",
            outcome: .done
        ))
        try await log.append(AuditEntry(
            jobID: "job-2",
            kind: .write,
            sampleType: "dietary_energy_consumed",
            outcome: .done
        ))

        let count = try await log.count()
        XCTAssertEqual(count, 2)

        // Reload from disk in a fresh actor instance.
        let reloaded = AuditLog(fileURL: url)
        let entries = try await reloaded.all()
        XCTAssertEqual(entries.count, 2)
        XCTAssertEqual(entries[0].jobID, "job-1")
        XCTAssertEqual(entries[1].sampleType, "dietary_energy_consumed")
    }

    func testCapEnforcedAtMaxEntries() async throws {
        let url = tempLogURL()
        let log = AuditLog(fileURL: url)
        let target = AuditLog.maxEntries + 50
        for i in 0..<target {
            try await log.append(AuditEntry(
                jobID: "job-\(i)",
                kind: .read,
                sampleType: "step_count",
                outcome: .done
            ))
        }
        let count = try await log.count()
        XCTAssertEqual(count, AuditLog.maxEntries)
        // The oldest entries should be evicted FIFO; the surviving range
        // is [50, target).
        let entries = try await log.all()
        XCTAssertEqual(entries.first?.jobID, "job-50")
        XCTAssertEqual(entries.last?.jobID, "job-\(target - 1)")
    }

    func testFailedEntryCarriesErrorCode() async throws {
        let url = tempLogURL()
        let log = AuditLog(fileURL: url)
        try await log.append(AuditEntry(
            jobID: "job-1",
            kind: .write,
            sampleType: "dietary_water",
            outcome: .failed,
            errorCode: "scope_denied"
        ))
        let entries = try await log.all()
        XCTAssertEqual(entries[0].outcome, .failed)
        XCTAssertEqual(entries[0].errorCode, "scope_denied")
    }

    func testClearWipesPersistedFile() async throws {
        let url = tempLogURL()
        let log = AuditLog(fileURL: url)
        try await log.append(AuditEntry(jobID: "j", kind: .read, sampleType: "step_count", outcome: .done))
        try await log.clear()
        do { let n = try await log.count(); XCTAssertEqual(n, 0) }

        let reloaded = AuditLog(fileURL: url)
        do { let n = try await reloaded.count(); XCTAssertEqual(n, 0) }
    }

    func testFreshLogAtNonexistentPathStartsEmpty() async throws {
        let log = AuditLog(fileURL: tempLogURL())
        do { let n = try await log.count(); XCTAssertEqual(n, 0) }
    }
}
