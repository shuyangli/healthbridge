# ios/

The iOS companion app and its testable Swift Package.

```
ios/
  Package.swift                   SwiftPM package — testable on macOS
  project.yml                     XcodeGen source for HealthBridge.xcodeproj
  Sources/HealthBridgeKit/        Pure Swift kit (relay client, codecs, types)
  Tests/HealthBridgeKitTests/     Kit unit tests
  HealthBridgeApp/                SwiftUI app sources (Info.plist, entitlements)
```

`HealthBridgeKit` is a Swift package that builds and tests on macOS without
HealthKit. It owns:

- The wire-format types that mirror `cli/internal/health/types.go`.
- A URLSession-backed `RelayClient` mirroring `cli/internal/relay`.
- The base64 `JobsCodec` (M2 will swap in XChaCha20-Poly1305).
- An `AnyCodable` shim for the polymorphic `Job.payload` field.

`HealthBridgeApp/` holds the SwiftUI app entry point, the foreground drain
loop, the HealthKit query glue, and the iOS-specific resources (Info.plist,
entitlements). Those files are wrapped in a thin Xcode project — see
`HealthBridgeApp/README.md` for setup.

## Test the kit

```sh
cd ios
swift test
```

## Build the iOS app

```sh
cd ios
brew install xcodegen     # one-time
xcodegen generate         # writes HealthBridge.xcodeproj from project.yml
xcodebuild -project HealthBridge.xcodeproj \
           -scheme HealthBridge \
           -sdk iphonesimulator \
           -destination 'generic/platform=iOS Simulator' build
```

The project file itself is gitignored — `project.yml` is the source of
truth, so PRs that touch the iOS structure stay reviewable as text.

## Run the app on a phone

Open `HealthBridge.xcodeproj` (after generating it) in Xcode, set the
development team in Signing & Capabilities, and edit the scheme's
environment variables to point at your local relay (`wrangler dev` in
`relay/`). The simulator does not have HealthKit data, so a real
device is needed for end-to-end testing.
