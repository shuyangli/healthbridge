# skill/healthbridge

A self-contained [agentskills.io](https://agentskills.io) skill that
teaches an LLM agent how to drive the `healthbridge` CLI to read and
write Apple Health data on the user's iPhone.

## Layout

```
healthbridge/
├── SKILL.md            Manifest the agent reads first (frontmatter + body)
├── README.md           This file (humans only)
├── references/
│   ├── COMMANDS.md     Per-command flag tables and JSON output shapes
│   └── TYPES.md        Sample type catalog and unit gotchas
└── examples/
    ├── log_meal.md     Worked example: logging calories
    └── weekly_summary.md  Worked example: reading recent activity
```

The agent loads `SKILL.md` once it's decided to activate the skill;
files under `references/` and `examples/` are loaded on demand when the
agent links to them.

## Prerequisites

1. The `healthbridge` Go binary on `PATH`. From this repo:
   ```sh
   brew install shuyangli/tap/healthbridge
   ```
2. The HealthBridge iOS app installed and paired with this Mac. Run
   `healthbridge pair` once and follow the QR-scan flow.
3. After pairing, the relay URL and pair ID are saved to
   `~/.healthbridge/config` automatically. No env vars needed.

## Installing with `npx skills`

To install the skill:

```sh
npx skills add shuyangli/healthbridge
```

To update the skill:

```sh
npx skills update healthbridge
```

## Installing from source (for any agentskills.io-compatible client)

The directory is portable. Drop it wherever your agent looks for
skills (`~/.config/<agent>/skills/`, an MCP skill directory, etc.).
The `SKILL.md` frontmatter validates against the agentskills.io
specification.

## Validate

```sh
# from https://github.com/agentskills/agentskills
skills-ref validate ./skill/healthbridge
```

## For development: updating the skill

When the CLI grows a new sample type or command:

1. `cd cli && go build ./...`
2. Run `healthbridge types --json` and update `references/TYPES.md`.
3. Run `healthbridge <new-command> --help` and update
   `references/COMMANDS.md`.
4. Bump `metadata.version` in `SKILL.md`.
