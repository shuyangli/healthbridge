# healthbridge CLI

The desktop side of HealthBridge. Encodes a job (read, write, sync),
seals it under the per-pair session key, pushes it to the relay, and
waits for the iOS app to drain it.

## Install

### From source via `go install` (recommended)

Requires Go 1.26 or newer.

```sh
go install github.com/shuyangli/healthbridge/cli/cmd/healthbridge@latest
```

This puts the binary at `$(go env GOPATH)/bin/healthbridge` (typically
`~/go/bin/healthbridge`). Make sure that directory is on your `PATH`:

```sh
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
exec zsh
```

Confirm:

```sh
healthbridge --help
```

### From a local checkout

If you have this repo cloned and want to install the working tree:

```sh
cd cli
go install ./cmd/healthbridge
```

Same install location as above.

### Pinning a specific version

```sh
go install github.com/shuyangli/healthbridge/cli/cmd/healthbridge@v0.1.0
```

Replace the tag with whatever version you want. `@latest` always grabs
the latest tagged release; `@main` grabs the current `main` branch tip.

### Building without installing

```sh
cd cli
go build -o ./bin/healthbridge ./cmd/healthbridge
./bin/healthbridge --help
```

Useful during development when you don't want to clobber your installed
copy.

## Configure

The CLI needs two pieces of state to talk to your iPhone:

1. **A relay URL.** Either pass `--relay <url>` on every command or
   export it once:
   ```sh
   export HEALTHBRIDGE_RELAY=https://healthbridge.shuyang-li.workers.dev
   ```
2. **A pair ID.** Get one by running `healthbridge pair` once (this
   shows a QR for the iOS app to scan). After successful pairing, the
   pair ID is printed to stdout and saved under
   `~/.config/healthbridge/pairs/<pair_id>.json`. Export it so you
   don't have to pass it every time:
   ```sh
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
