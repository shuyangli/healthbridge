# HealthBridgeApp

The SwiftUI iOS companion app. Source files in this directory rely on
`import HealthKit` + `import SwiftUI` and so only build inside an
Xcode project targeting iOS 17+.

The HealthKit-free heart of the app — the relay client, the job/result
codec, the crypto, the audit log — lives in the
[`HealthBridgeKit`](../Package.swift) Swift package next to this folder
and is exercised by `swift test` on macOS.

## Generate the Xcode project

The `.xcodeproj` is gitignored. The source of truth is
[`../project.yml`](../project.yml), processed by
[XcodeGen](https://github.com/yonaskolb/XcodeGen):

```sh
brew install xcodegen
cd ios
xcodegen generate
open HealthBridge.xcodeproj
```

Re-run `xcodegen generate` whenever `project.yml` changes; the
generator is fast and idempotent.

## Build for the simulator

```sh
cd ios
xcodegen generate
xcodebuild -project HealthBridge.xcodeproj \
           -scheme HealthBridge \
           -sdk iphonesimulator \
           -destination 'generic/platform=iOS Simulator' build
```

The simulator will not have any HealthKit data, but it's enough to
verify the project compiles and the relay drain loop runs against a
local `wrangler dev` of the relay.

## Run on a real iPhone

1. Open `HealthBridge.xcodeproj` in Xcode.
2. Set the development team in Signing & Capabilities (Personal Team
   is fine for sideloading).
3. Edit the scheme to set `HEALTHBRIDGE_PAIR`, `HEALTHBRIDGE_RELAY`,
   and `HEALTHBRIDGE_KEY` environment variables to match a local
   pairing you set up via the CLI.
4. Run on your phone.

## Why the hand-written Info.plist + entitlements

The Info.plist ships the `NSHealthShareUsageDescription` and
`NSHealthUpdateUsageDescription` strings App Review reads, plus the
device-capability `healthkit` entry. The entitlements file declares the
`com.apple.developer.healthkit` capability.

`project.yml` is configured **not** to auto-generate either of these
files (`GENERATE_INFOPLIST_FILE: NO` and no `info:` / `entitlements:`
target blocks), so the hand-written copies survive `xcodegen generate`.
Both are wired through `INFOPLIST_FILE` and `CODE_SIGN_ENTITLEMENTS`
build settings instead.
