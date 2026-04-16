import XCTest
@testable import HealthBridgeKit

final class ConnectionStatusTests: XCTestCase {

    // MARK: - effectiveConnectionStatus

    func testEffectiveStatus_networkAvailable_passesThrough() {
        for base: ConnectionStatus in [
            .notPaired, .connecting, .connected,
            .backgrounded, .networkUnavailable,
            .relayError("boom"),
        ] {
            XCTAssertEqual(
                effectiveConnectionStatus(base: base, networkAvailable: true),
                base,
                "should pass through \(base) when network is available"
            )
        }
    }

    func testEffectiveStatus_networkDown_overridesConnectedAndConnecting() {
        XCTAssertEqual(
            effectiveConnectionStatus(base: .connected, networkAvailable: false),
            .networkUnavailable
        )
        XCTAssertEqual(
            effectiveConnectionStatus(base: .connecting, networkAvailable: false),
            .networkUnavailable
        )
    }

    func testEffectiveStatus_networkDown_preservesOtherStates() {
        for base: ConnectionStatus in [
            .notPaired, .backgrounded, .networkUnavailable,
            .relayError("x"),
        ] {
            XCTAssertEqual(
                effectiveConnectionStatus(base: base, networkAvailable: false),
                base,
                "should preserve \(base) when network is down"
            )
        }
    }

    // MARK: - classifyDrainError()

    func testDrainError_cancellationError_returnsNil() {
        let err = CancellationError()
        XCTAssertNil(classifyDrainError( err))
    }

    func testDrainError_urlErrorCancelled_returnsNil() {
        let err = URLError(.cancelled)
        XCTAssertNil(classifyDrainError( err))
    }

    func testDrainError_notConnectedToInternet_returnsNetworkUnavailable() {
        let err = URLError(.notConnectedToInternet)
        XCTAssertEqual(classifyDrainError( err), .networkUnavailable)
    }

    func testDrainError_networkConnectionLost_returnsNetworkUnavailable() {
        let err = URLError(.networkConnectionLost)
        XCTAssertEqual(classifyDrainError( err), .networkUnavailable)
    }

    func testDrainError_dataNotAllowed_returnsNetworkUnavailable() {
        let err = URLError(.dataNotAllowed)
        XCTAssertEqual(classifyDrainError( err), .networkUnavailable)
    }

    func testDrainError_otherURLError_returnsRelayError() {
        let err = URLError(.badServerResponse)
        let result = classifyDrainError( err)
        guard case .relayError = result else {
            XCTFail("expected .relayError, got \(String(describing: result))")
            return
        }
    }

    func testDrainError_arbitraryError_returnsRelayError() {
        struct TestError: Error, LocalizedError {
            var errorDescription: String? { "test failure" }
        }
        let result = classifyDrainError(TestError())
        XCTAssertEqual(result, ConnectionStatus.relayError("test failure"))
    }
}
