# CLI distribution

- Owner: shuyangli
- Last updated: 2026-04-07
- Current status: in progress — GoReleaser config and release workflow landed; next action is to create the `shuyangli/homebrew-tap` repo, mint a fine-grained PAT, store it as the `HOMEBREW_TAP_GITHUB_TOKEN` Actions secret, and cut `v0.1.0`.

## Motivation

Today the only way to install `healthbridge` is `go install
github.com/shuyangli/healthbridge/cli/cmd/healthbridge@latest`, which
requires a Go 1.26+ toolchain and gives the user no semver discipline,
no signed artifacts, and no upgrade story. We want a one-line install
for the agent's primary audience (Mac users running Claude Code), without
taking on the cost of an Apple Developer subscription.

## Design overview

Two install paths fed by one release pipeline:

1. **Homebrew tap** at `shuyangli/homebrew-tap` is the headline path.
   `brew install shuyangli/tap/healthbridge`. Homebrew strips the
   macOS quarantine attribute on install, so we sidestep Gatekeeper
   without needing Apple Developer ID signing.
2. **GitHub Releases** carry the prebuilt tarballs (darwin/linux,
   amd64/arm64) that the tap formula points at, and serve as the
   fallback for Linux users and anyone who wants to `curl` a binary.
3. `go install` is kept around as the dev/impatient path, demoted in
   the README.

GoReleaser drives all of (1) and (2) from a single tag push. The
release workflow runs on `ubuntu-latest` (modernc.org/sqlite is pure
Go, so cross-compilation is CGO-free).

## Details

### Tagging

Plain `v*` tags. The CLI is the only released artifact today; if/when
the relay or iOS app start cutting independent releases we can switch
to `cli/v*` and turn on GoReleaser's `monorepo:` config.

### Versioning of the binary

`healthbridge --version` and `healthbridge version [--json]` are wired
to package-level vars `Version`, `Commit`, `Date` in
`cli/cmd/healthbridge/cmd/version.go`, populated via `-ldflags -X`
during the GoReleaser build. When the binary is built without
`-ldflags` (e.g. `go install`), the command falls back to
`debug.ReadBuildInfo()` so the VCS revision still surfaces.

### GoReleaser config

Lives at `cli/.goreleaser.yaml`. The workflow runs `goreleaser` with
`workdir: cli` so it sees `cli/go.mod` directly. Builds:

- `darwin/amd64`, `darwin/arm64`
- `linux/amd64`, `linux/arm64`

Archive name template: `healthbridge_{{ .Version }}_{{ .Os }}_{{ .Arch }}`.
Checksums file is published alongside.

### Brew formula

GoReleaser's `brews:` block auto-generates the formula and pushes a
PR to `shuyangli/homebrew-tap` on every release using the
`HOMEBREW_TAP_GITHUB_TOKEN` secret. Formula `test do` block runs
`#{bin}/healthbridge --version` to confirm the binary launches on the
target machine — that exercises Cobra wiring without touching the
relay or HealthKit.

### CI workflow

`.github/workflows/release.yml` triggers on `v*` tag pushes only. It
checks out with `fetch-depth: 0` (GoReleaser needs full history for
the changelog), sets up Go 1.26.1 explicitly via `actions/setup-go`,
and runs `goreleaser release --clean` from `cli/`.

## Risks and mitigations

- **No LICENSE file at the repo root.** The skill manifest declares
  MIT, but the formula's `license "MIT"` field is currently a claim
  the repo doesn't back up. Add a `LICENSE` file before tagging
  `v0.1.0`. Tracked separately — this design doc does not create one.
- **Go 1.26.1 is recent.** `actions/setup-go` should resolve it, but
  if the runner image lags we can pin to a slightly older patch
  version, or lower `go.mod`'s directive. The user has approved
  going down to an explicit version if needed.
- **Apple Gatekeeper on direct downloads.** Homebrew bypasses this;
  users who download tarballs directly will need
  `xattr -d com.apple.quarantine ./healthbridge`. Documented in the
  README. We revisit Apple notarization only if direct downloads
  become the dominant install path.
- **Tap repo bootstrap is manual one-time work.** Creating the
  `shuyangli/homebrew-tap` repo and minting the PAT can't be
  automated from this repo. Documented in the milestone checklist.
- **Versioning discipline starts now.** Once `v0.1.0` ships, brew
  users will pin against it. Pre-1.0 we treat the wire format as
  unstable; bumping the minor (`v0.2.0`, etc.) is the signal that
  jobs/results may have changed shape.

## Milestones

1. **[done]** Add `cli/.goreleaser.yaml`, `.github/workflows/release.yml`,
   and `cli/cmd/healthbridge/cmd/version.go`. Update READMEs to lead
   with brew install.
2. **[pending — user action]** Create `shuyangli/homebrew-tap` repo
   with an initial commit (`README.md` is enough). Mint a fine-grained
   PAT scoped to that repo with `Contents: read/write` and
   `Pull requests: read/write`. Add it as
   `HOMEBREW_TAP_GITHUB_TOKEN` in this repo's Actions secrets.
3. **[pending]** Add a `LICENSE` file (MIT) to the repo root so the
   formula's license claim is honest.
4. **[pending]** Tag `v0.1.0` (`git tag v0.1.0 && git push --tags`).
   Watch the workflow, confirm the tap PR opens and merges, and
   `brew install shuyangli/tap/healthbridge` end-to-end on a clean
   machine.
5. **[pending]** Update `cli/README.md` and root `README.md` to point
   at the published brew install line once it actually resolves.
6. **[deferred]** Apple Developer ID signing + notarization. Only
   worth it if direct (non-brew) downloads become the dominant path.
