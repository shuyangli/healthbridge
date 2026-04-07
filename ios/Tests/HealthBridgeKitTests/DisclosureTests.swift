import XCTest
@testable import HealthBridgeKit

/// These tests pin the in-app disclosure text against accidental regression.
/// Apple's App Store guideline 5.1.2(i) requires explicit disclosure that
/// personal data is shared with third-party AI; if a refactor removes the
/// "AI" keyword from the headline or body the app could fail review, so
/// the strings are exercised here as part of CI rather than relying on a
/// human review of every PR that touches Disclosure.swift.
final class DisclosureTests: XCTestCase {

    func testHeadlineMentionsAI() {
        XCTAssertTrue(Disclosure.pairingHeadline.contains("AI"))
    }

    func testBodyMentionsAIAndEncryption() {
        XCTAssertTrue(Disclosure.pairingBody.contains("AI"))
        XCTAssertTrue(Disclosure.pairingBody.localizedCaseInsensitiveContains("encrypted"))
    }

    func testNonGoalsCoverTheKeyExclusions() {
        let joined = Disclosure.pairingNonGoals.joined(separator: " ").lowercased()
        for required in ["advertising", "icloud", "third-party"] {
            XCTAssertTrue(joined.contains(required), "non-goals missing %@".replacingOccurrences(of: "%@", with: required))
        }
    }

    func testSASPromptAsksForConfirmation() {
        XCTAssertTrue(Disclosure.sasPrompt.localizedCaseInsensitiveContains("confirm"))
    }
}
