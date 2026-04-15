# healthbridge CLI

The desktop side of HealthBridge. Encodes a job (read, write, sync),
seals it under the per-pair session key, pushes it to the relay, and
waits for the iOS app to drain it.

## Install

### Homebrew (recommended on macOS)

The CLI is published through the
[`shuyangli/tap`](https://github.com/shuyangli/homebrew-tap)
Homebrew tap. Tap it once and install:

```sh
brew tap shuyangli/tap
brew install healthbridge
```

Or in one shot, which auto-taps as a side effect:

```sh
brew install shuyangli/tap/healthbridge
```

Confirm:

```sh
healthbridge --version
# healthbridge 0.0.3 (<sha>, <date>) darwin/arm64
```

Future releases come down via:

```sh
brew upgrade healthbridge
```

The CLI is shipped as a Homebrew **cask**, not a formula, because it's
a prebuilt binary. Casks normally quarantine downloaded binaries by
default, which would make Gatekeeper kill the CLI on first launch
(we're not Apple Developer ID-signed). The cask's `postflight` block
clears the quarantine xattr automatically on install, so the binary
just runs — no `xattr` dance needed.

### Prebuilt tarball from GitHub Releases

For Linux, or for macOS users who don't want Homebrew, grab the
matching tarball from
[the Releases page](https://github.com/shuyangli/healthbridge/releases):

```sh
VERSION=0.0.3
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')
curl -L "https://github.com/shuyangli/healthbridge/releases/download/v${VERSION}/healthbridge_${VERSION}_${OS}_${ARCH}.tar.gz" \
  | tar -xz
sudo install ./healthbridge /usr/local/bin/healthbridge
```

On macOS, if you downloaded the tarball through a browser instead of
`curl`, Gatekeeper will quarantine it. Clear the attribute once:

```sh
xattr -d com.apple.quarantine /usr/local/bin/healthbridge
```

(Homebrew users don't have to do this — the cask postflight handles it.)

### From source via `go install`

Requires Go 1.26 or newer.

```sh
go install github.com/shuyangli/healthbridge/cli/cmd/healthbridge@latest
export PATH="$HOME/go/bin:$PATH"
```

`@latest` tracks the most recent tagged release; `@main` builds from
the current `main` branch tip.

### From a local checkout (development)

```sh
cd cli
go install ./cmd/healthbridge
# or, without clobbering your installed copy:
go build -o ./bin/healthbridge ./cmd/healthbridge
```

## Configure

The CLI needs two pieces of state to talk to your iPhone:

1. **A relay URL.** Set up your own Cloudflare Worker as the relay
   first — see [`../relay/README.md`](../relay/README.md) for the
   deploy steps. Then either pass `--relay <url>`, export
   `HEALTHBRIDGE_RELAY`, or let `healthbridge pair` save it as the
   active default in `~/.healthbridge/config`. The relay is a dumb
   store-and-forward mailbox that only ever sees ciphertext.
2. **A pair ID.** Get one by running `healthbridge pair` once (this
   shows a QR for the iOS app to scan). After successful pairing, the
   full session is saved under `~/.config/healthbridge/pairs/<pair_id>.json`
   and the active pair ID is saved under `~/.healthbridge/config`, so
   follow-up commands can usually just call `healthbridge ...` directly.

If you want to override the active default for a shell or script, env vars
still win:

```sh
export HEALTHBRIDGE_RELAY=https://<your-worker>.workers.dev
export HEALTHBRIDGE_PAIR=01J...
```

## First-run sanity check

```sh
healthbridge pair --relay https://<your-worker>.workers.dev
# scan the QR with the HealthBridge iOS app, confirm SAS on both sides

healthbridge status --json
healthbridge types --json

healthbridge write dietary_water --value 250 --unit mL --at now --json
healthbridge read step_count --from -1d --json
```

## Commands

Run `healthbridge help` for the full command list, or `healthbridge <command> --help` for per-command flags. The agent skill at [`../skill/healthbridge/references/COMMANDS.md`](../skill/healthbridge/references/COMMANDS.md) documents JSON output shapes.

## Layout

```
cmd/healthbridge/        Cobra entry point and subcommands
internal/crypto/         X25519 + HKDF + ChaCha20-Poly1305
internal/pairing/        Pairing protocol (initiator + responder)
internal/relay/          HTTP client + fakerelay test double
internal/jobs/           SQLite job mirror + sealed-job codec
internal/cache/          SQLite cache + per-type anchors for sync
internal/health/         Wire types (Job, Sample, payloads)
internal/config/         On-disk pair records
hack/                    Helper scripts
```

## Run the tests

```sh
cd cli
go test ./...
```

Race detector:

```sh
go test -race ./...
```

## Uninstall

```sh
rm "$(go env GOPATH)/bin/healthbridge"
rm -rf ~/.config/healthbridge       # pair records (irreversible)
rm -rf "$XDG_DATA_HOME/healthbridge" # job mirror + sample cache
```
