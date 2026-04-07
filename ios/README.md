# ios/

The iOS companion app and its testable Swift Package.

```
ios/
  Package.swift                   SwiftPM package — testable on macOS
  Sources/HealthBridgeKit/        Pure Swift kit (relay client, codecs, types)
  Tests/HealthBridgeKitTests/     Kit unit tests
  HealthBridgeApp/                SwiftUI app sources (need Xcode project)
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

## Run the app on a phone

Open the Xcode project, set the scheme's environment variables to point at
your local relay (`wrangler dev` in `relay/`), and Run on a real device.
The simulator does not have HealthKit data.
