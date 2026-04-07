// Disclosure holds the user-facing strings the iOS app shows during the
// pairing flow to satisfy App Store guideline 5.1.2(i). They are
// embedded as constants here (rather than loaded from PRIVACY.md at
// runtime) so the kit has zero filesystem dependencies and the in-app
// text is reviewable in source.

import Foundation

public enum Disclosure {

    /// Headline shown on the pairing-confirmation screen, before the user
    /// can grant any HealthKit scopes. Must mention "AI" in the body.
    public static let pairingHeadline = "Share your Health data with an AI agent on your Mac"

    /// Body paragraph immediately under the headline.
    public static let pairingBody = """
    HealthBridge will share the HealthKit sample types you choose with the \
    AI agent running on your paired Mac. The data is end-to-end encrypted: \
    the relay that brokers the connection only sees ciphertext, never your \
    Health data in the clear.

    You can revoke the pair, change which sample types are shared, or read \
    the full activity log at any time from the HealthBridge app on this \
    iPhone.
    """

    /// Bullet points enumerating what HealthBridge does NOT do.
    public static let pairingNonGoals: [String] = [
        "No advertising. No analytics. No telemetry.",
        "No iCloud sync of Health data.",
        "No third-party AI services beyond the agent on your paired Mac.",
        "No false or generated entries — only what the agent asks you for."
    ]

    /// Confirmation prompt rendered alongside the SAS digits.
    public static let sasPrompt = "Confirm that the same six-digit code is shown on your Mac before continuing."

    /// Footer link/label that points the user at the in-repo PRIVACY.md.
    public static let privacyLinkLabel = "Read the full privacy policy"
}
