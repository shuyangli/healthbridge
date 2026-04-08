# healthbridge CLI

The desktop side of HealthBridge. Encodes a job (read, write, sync),
seals it under the per-pair session key, pushes it to the relay, and
waits for the iOS app to drain it.

## Install

### Homebrew (recommended on macOS)

```sh
brew install shuyangli/tap/healthbridge
healthbridge --version
```

Updates flow through `brew upgrade healthbridge`. Homebrew strips the
macOS quarantine attribute on install, so Gatekeeper won't second-guess
the binary the way it does for a manually-downloaded tarball.

### Prebuilt tarball from GitHub Releases

For Linux, or for macOS users who don't want Homebrew, grab the
matching tarball from
[the Releases page](https://github.com/shuyangli/healthbridge/releases):

```sh
VERSION=0.0.1
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

(Homebrew users don't have to do this.)

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

1. **A relay URL.** Either pass `--relay <url>`, export
   `HEALTHBRIDGE_RELAY`, or let `healthbridge pair` save it as the active
   default in `~/.healthbridge/config`.
2. **A pair ID.** Get one by running `healthbridge pair` once (this
   shows a QR for the iOS app to scan). After successful pairing, the
   full session is saved under `~/.config/healthbridge/pairs/<pair_id>.json`
   and the active pair ID is saved under `~/.healthbridge/config`, so
   follow-up commands can usually just call `healthbridge ...` directly.

If you want to override the active default for a shell or script, env vars
still win:

```sh
export HEALTHBRIDGE_RELAY=https://healthbridge.shuyang-li.workers.dev
export HEALTHBRIDGE_PAIR=01J...
```

## First-run sanity check

```sh
healthbridge pair --relay https://healthbridge.shuyang-li.workers.dev
# scan the QR with the HealthBridge iOS app, confirm SAS on both sides

healthbridge status --json
healthbridge types --json

healthbridge write dietary_water --value 250 --unit mL --at now --json
healthbridge read step_count --from -1d --json
```

## Command surface

```
healthbridge read    <type> [--from -7d] [--to now] [--limit N]
healthbridge write   <type> --value <n> --unit <u> [--at <t>] [--meta k=v]
healthbridge sync    [--type <t>...] [--full]
healthbridge jobs    list|get|wait|cancel|prune
healthbridge status
healthbridge scopes  list|grant|revoke
healthbridge types
healthbridge pair
healthbridge wipe    [--yes]
healthbridge version [--json]
```

`healthbridge <command> --help` for full flag listings, or see the
agent skill at [`../skill/healthbridge/references/COMMANDS.md`](../skill/healthbridge/references/COMMANDS.md)
for per-command JSON output shapes.

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
