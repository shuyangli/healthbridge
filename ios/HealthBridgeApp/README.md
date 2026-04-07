# HealthBridgeApp

The SwiftUI iOS companion app. The source files in this directory rely
on `import HealthKit` + `import SwiftUI` and so only build inside an
Xcode project targeting iOS 17+.

The HealthKit-free heart of the app — the relay client, the job/result
codec, and the wire-format types — lives in the
[`HealthBridgeKit`](../Package.swift) Swift package next to this folder
and is exercised by `swift test` on macOS.

## Set up the Xcode project (one-time)

```sh
cd ios
# 1. File → New → Project → iOS → App
#    Product Name: HealthBridge
#    Interface: SwiftUI, Language: Swift
#    Bundle Identifier: dev.shuyangli.healthbridge
# 2. Delete the auto-generated ContentView.swift / *App.swift
# 3. Drag HealthBridgeApp.swift into the project
# 4. Set Info.plist to HealthBridgeApp/Info.plist
# 5. Set Code Signing Entitlements to HealthBridgeApp/HealthBridge.entitlements
# 6. Add the local Swift package: File → Add Package Dependencies → Add Local… → ../
#    Then add the HealthBridgeKit library to the target
# 7. Add the HealthKit capability under Signing & Capabilities
```

After that, building HealthBridge for any iOS device should compile and
launch the foreground drain loop. Set `HEALTHBRIDGE_PAIR` and
`HEALTHBRIDGE_RELAY` in the scheme environment for M1 testing.

## Why no `.xcodeproj` in version control?

`.xcodeproj` files contain absolute paths and developer-specific cruft
that bloats diffs and causes merge conflicts. Generating it locally from
the source files in this folder + the package next door keeps git clean.
A future commit may add an `XcodeGen` `project.yml` so the project can
be regenerated reproducibly.
