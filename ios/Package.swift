// swift-tools-version:5.9
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
        .iOS(.v17),
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
