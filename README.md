# HealthBridge

A CLI + iOS app + serverless relay that lets local AI agents read and write
Apple Health data.

The desktop CLI (`healthbridge`, in [`cli/`](cli/)) talks to a tiny Cloudflare
Worker relay (in [`relay/`](relay/)). The iOS companion app (in [`ios/`](ios/))
owns HealthKit and drains the relay's encrypted job mailbox whenever it is
foregrounded.

The relay is dumb store-and-forward and never sees plaintext Health data —
all jobs and results are end-to-end encrypted between the CLI and the iOS app
with a key established at pairing time.

## Install the CLI

On macOS, install via the [`shuyangli/tap`](https://github.com/shuyangli/homebrew-tap)
Homebrew tap:

```sh
brew tap shuyangli/tap
brew install healthbridge

# or, in one shot:
brew install shuyangli/tap/healthbridge

healthbridge --version
healthbridge pair --relay https://<your-worker>.workers.dev
# scan the QR with the HealthBridge iOS app
```

You'll need to set up your own Cloudflare Worker as the relay — see
[`relay/README.md`](relay/README.md) for the deploy steps. The relay
is a dumb store-and-forward mailbox that only ever sees ciphertext, so
running your own instance is the safest deployment.

Optionally, you can enable push notifications so the iOS app wakes up
automatically when a job is enqueued. This requires an Apple Developer
account and an APNs authentication key — see the
[APNs setup section](relay/README.md#apple-push-notifications-apns) in
the relay README. Without push notifications the app falls back to
polling when foregrounded.

`brew upgrade healthbridge` picks up future releases.

Linux users and anyone who wants a tarball can grab one from
[GitHub Releases](https://github.com/shuyangli/healthbridge/releases).
Go developers can still `go install
github.com/shuyangli/healthbridge/cli/cmd/healthbridge@latest`.

See [`cli/README.md`](cli/README.md) for the full install, configure,
and first-run walkthrough.

## Install the iOS app

The iOS app is not on the App Store — side-load it onto your iPhone
by building from Xcode. An Apple Developer account (free or paid) is
required for signing.

```sh
cd ios

# Set your signing identity and bundle ID:
export HEALTHBRIDGE_TEAM_ID="<your Apple Developer Team ID>"
export HEALTHBRIDGE_BUNDLE_ID_PREFIX="com.<yourname>"
export HEALTHBRIDGE_BUNDLE_ID="com.<yourname>.HealthBridge"

# Generate the Xcode project and open it:
xcodegen
open HealthBridge.xcodeproj
```

Then connect your iPhone (or select it under *Destination*) and hit
**Cmd+R** to build and run.

See [`ios/README.md`](ios/README.md) for more details.

## Install the agent skill

After installing the CLI, add the HealthBridge agent skill so your AI
agent knows how to drive it:

```sh
npx skills add shuyangli/healthbridge
```

See [`skill/healthbridge/README.md`](skill/healthbridge/README.md) for
alternative install methods.


## Limitations

- HealthBridge is not designed for bulk exports; to get a comprehensive export
  of Apple Health data for advanced analysis, please use the "Export All
  Health Data" button in Apple Health and provide the zip file to your agent.
- Cloudflare Worker has [storage limits](https://developers.cloudflare.com/durable-objects/platform/limits/#sqlite-backed-durable-objects-general-limits) on Durable Objects, so extremely
  large queries may fail.
