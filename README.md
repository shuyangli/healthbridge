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

`brew upgrade healthbridge` picks up future releases.

Linux users and anyone who wants a tarball can grab one from
[GitHub Releases](https://github.com/shuyangli/healthbridge/releases).
Go developers can still `go install
github.com/shuyangli/healthbridge/cli/cmd/healthbridge@latest`.

See [`cli/README.md`](cli/README.md) for the full install, configure,
and first-run walkthrough.

## Limitations

- HealthBridge is not designed for bulk exports; to get a comprehensive export
  of Apple Health data for advanced analysis, please use the "Export All
  Health Data" button in Apple Health and provide the zip file to your agent.
- Cloudflare Worker has [storage limits](https://developers.cloudflare.com/durable-objects/platform/limits/#sqlite-backed-durable-objects-general-limits) on Durable Objects, so extremely
  large queries may fail.
