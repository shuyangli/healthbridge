import XCTest
@testable import HealthBridgeKit

/// Tests for the generated SampleTypeCatalog. These mirror the
/// Layer-1 invariants the Go-side `cli/internal/health/catalog_test.go`
/// enforces, so any drift between the two languages surfaces here as
/// well as in the gen-types stability check.
final class SampleTypeCatalogTests: XCTestCase {

    func testAllKnownIsAllQuantityPlusCarryover() {
        let allKnown = Set(SampleType.allKnown.map { $0.rawValue })
        let allQuantity = Set(SampleType.allQuantity.map { $0.rawValue })
        XCTAssertTrue(allQuantity.isSubset(of: allKnown))
        XCTAssertEqual(allKnown.subtracting(allQuantity), ["sleep_analysis", "workout"])
    }

    func testCatalogSizeIsAtLeast100() {
        // Defensive: if a category accidentally drops out of the
        // generator the count would crater.
        XCTAssertGreaterThanOrEqual(SampleType.allQuantity.count, 100)
    }

    func testNoDuplicateRawValues() {
        var seen = Set<String>()
        for s in SampleType.allKnown {
            XCTAssertFalse(seen.contains(s.rawValue), "duplicate rawValue \(s.rawValue)")
            seen.insert(s.rawValue)
        }
    }

    func testShippedConstantsArePresent() {
        // Pin the wire names that have already been released, so any
        // future rename of a Go-side constant that breaks the
        // round-trip surfaces here. Mirrors the Go-side
        // TestCatalogPreservesShippedWireNames.
        let pinned: [(String, SampleType)] = [
            ("step_count", .stepCount),
            ("active_energy_burned", .activeEnergyBurned),
            ("basal_energy_burned", .basalEnergyBurned),
            ("heart_rate", .heartRate),
            ("heart_rate_resting", .heartRateResting),
            ("body_mass", .bodyMass),
            ("body_mass_index", .bodyMassIndex),
            ("body_fat_percentage", .bodyFatPercentage),
            ("lean_body_mass", .leanBodyMass),
            ("height", .height),
            ("blood_glucose", .bloodGlucose),
            ("dietary_energy_consumed", .dietaryEnergyConsumed),
            ("dietary_protein", .dietaryProtein),
            ("dietary_carbohydrates", .dietaryCarbohydrates),
            ("dietary_fat_total", .dietaryFatTotal),
            ("dietary_fat_saturated", .dietaryFatSaturated),
            ("dietary_fiber", .dietaryFiber),
            ("dietary_sugar", .dietarySugar),
            ("dietary_cholesterol", .dietaryCholesterol),
            ("dietary_sodium", .dietarySodium),
            ("dietary_caffeine", .dietaryCaffeine),
            ("dietary_water", .dietaryWater),
            ("sleep_analysis", .sleepAnalysis),
            ("workout", .workout),
        ]
        for (wire, constant) in pinned {
            XCTAssertEqual(constant.rawValue, wire, "constant for \(wire) drifted")
        }
    }

    func testSampleTypeJSONRoundTrip() throws {
        // SampleType is a struct now, not an enum — manual Codable.
        // Verify it still encodes as a bare JSON string for every
        // catalog entry.
        let encoder = JSONEncoder()
        let decoder = JSONDecoder()
        for s in SampleType.allKnown {
            let data = try encoder.encode(s)
            let decoded = try decoder.decode(SampleType.self, from: data)
            XCTAssertEqual(decoded, s)
            XCTAssertEqual(String(data: data, encoding: .utf8), "\"\(s.rawValue)\"")
        }
    }

    func testSampleEncodingPreservesNewType() throws {
        // Spot-check a brand-new catalog type round-trips through
        // Sample's full JSON encoding (the iOS app's read drain
        // path).
        let sample = Sample(
            uuid: "abc",
            type: .runningPower,
            value: 280.5,
            unit: "W",
            start: ISO8601DateFormatter().date(from: "2026-04-07T12:00:00Z")!,
            end: ISO8601DateFormatter().date(from: "2026-04-07T12:00:01Z")!
        )
        let data = try JSONEncoder.iso8601.encode(sample)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("\"type\":\"running_power\""))
        XCTAssertTrue(json.contains("\"unit\":\"W\""))

        let decoded = try JSONDecoder.iso8601.decode(Sample.self, from: data)
        XCTAssertEqual(decoded.type, .runningPower)
        XCTAssertEqual(decoded.unit, "W")
    }
}
