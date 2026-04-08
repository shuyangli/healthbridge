import XCTest
@testable import HealthBridgeKit

/// Round-trip and shape tests for the CharacteristicType wire model.
/// The HKCharacteristicType↔snake_case mapping (HKBiologicalSex.female
/// → "female", HKBloodType.aPositive → "a_positive", …) lives in the
/// iOS app target and gets covered by HealthKitMappingTests there;
/// these kit-side tests cover the JSON serialisation and the typed
/// Job/ProfilePayload helpers.
final class CharacteristicTypeTests: XCTestCase {

    func testCharacteristicTypeJSONEncodesAsBareString() throws {
        let cases: [(CharacteristicType, String)] = [
            (.dateOfBirth, "\"date_of_birth\""),
            (.biologicalSex, "\"biological_sex\""),
            (.bloodType, "\"blood_type\""),
            (.fitzpatrickSkinType, "\"fitzpatrick_skin_type\""),
            (.wheelchairUse, "\"wheelchair_use\""),
            (.activityMoveMode, "\"activity_move_mode\""),
        ]
        let encoder = JSONEncoder()
        for (c, want) in cases {
            let data = try encoder.encode(c)
            XCTAssertEqual(String(data: data, encoding: .utf8), want)
        }
    }

    func testProfilePayloadAndResultRoundTrip() throws {
        let payload = ProfilePayload(field: .biologicalSex)
        let data = try JSONEncoder().encode(payload)
        let json = String(data: data, encoding: .utf8)!
        XCTAssertTrue(json.contains("\"field\":\"biological_sex\""))

        let decoded = try JSONDecoder().decode(ProfilePayload.self, from: data)
        XCTAssertEqual(decoded, payload)

        let result = ProfileResult(field: .bloodType, value: "a_positive")
        let resultData = try JSONEncoder().encode(result)
        let resultJSON = String(data: resultData, encoding: .utf8)!
        XCTAssertTrue(resultJSON.contains("\"field\":\"blood_type\""))
        XCTAssertTrue(resultJSON.contains("\"value\":\"a_positive\""))
        let decodedResult = try JSONDecoder().decode(ProfileResult.self, from: resultData)
        XCTAssertEqual(decodedResult, result)
    }

    func testProfileJobDecodeRefusesWrongKind() throws {
        let readJob = Job(
            id: "01J9ZX0EXAMPLE0000000000001",
            kind: .read,
            createdAt: Date(),
            payload: try AnyCodable.from(ReadPayload(
                type: .stepCount,
                from: Date(),
                to: Date()
            ))
        )
        XCTAssertThrowsError(try readJob.decodeProfilePayload()) { err in
            guard case JobDecodeError.wrongKind(let expected, let actual) = err else {
                return XCTFail("expected wrongKind, got \(err)")
            }
            XCTAssertEqual(expected, .profile)
            XCTAssertEqual(actual, .read)
        }
    }

    func testProfileJobDecodeRoundTrip() throws {
        let payload = ProfilePayload(field: .dateOfBirth)
        let job = Job(
            id: "01J9ZX0EXAMPLE0000000000002",
            kind: .profile,
            createdAt: Date(),
            payload: try AnyCodable.from(payload)
        )
        let decoded = try job.decodeProfilePayload()
        XCTAssertEqual(decoded.field, .dateOfBirth)
    }

    func testCharacteristicAllKnownContainsEverything() {
        let raws = Set(CharacteristicType.allKnown.map { $0.rawValue })
        XCTAssertEqual(raws, [
            "date_of_birth",
            "biological_sex",
            "blood_type",
            "fitzpatrick_skin_type",
            "wheelchair_use",
            "activity_move_mode",
        ])
    }
}
