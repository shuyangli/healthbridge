// swift-tools-version:6.0
//
// HealthBridgeKit is the testable, HealthKit-free heart of the iOS app:
// the relay client, the job/result codecs, and the data model. It builds on
// macOS so we can `swift test` it without an iOS simulator.
//
// The actual SwiftUI iOS app lives under HealthBridgeApp/ and is wrapped in
// an Xcode project (see HealthBridgeApp/README.md). It depends on this
// package for everything that doesn't touch HealthKit.

import PackageDescription

let package = Package(
    name: "HealthBridgeKit",
    platforms: [
        // iOS 18 because the catalog (Sources/HealthBridgeKit/Generated/
        // SampleTypeCatalog.swift) references HKQuantityTypeIdentifiers
        // that ship in iOS 18 (running/cycling/rowing/swimming distances
        // for new modalities, workoutEffortScore, etc.). Older targets
        // would need @available annotations on every iOS-18-gated
        // catalog entry.
        .iOS(.v18),
        .macOS(.v13),
    ],
    products: [
        .library(name: "HealthBridgeKit", targets: ["HealthBridgeKit"]),
    ],
    targets: [
        .target(
            name: "HealthBridgeKit",
            path: "Sources/HealthBridgeKit"
        ),
        .testTarget(
            name: "HealthBridgeKitTests",
            dependencies: ["HealthBridgeKit"],
            path: "Tests/HealthBridgeKitTests"
        ),
    ]
)
