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
   cd cli && go install ./cmd/healthbridge
   export PATH="$HOME/go/bin:$PATH"
   ```
2. The HealthBridge iOS app installed and paired with this Mac. Run
   `healthbridge pair` once and follow the QR-scan flow.
3. The relay URL exported so the agent doesn't have to guess:
   ```sh
   export HEALTHBRIDGE_RELAY=https://healthbridge.shuyang-li.workers.dev
   export HEALTHBRIDGE_PAIR=01J...   # ULID printed by `healthbridge status`
   ```

## Install (Hermes)

The agentskills.io format is consumed by Hermes via either a local
directory or a checkout from a community hub. The simplest path:

```sh
mkdir -p ~/.hermes/skills
cp -r skill/healthbridge ~/.hermes/skills/healthbridge
```

Restart Hermes (or trigger a skill reload) and ask it something
HealthKit-related. The skill's `description` field is what Hermes uses
to decide when to activate it, so phrasings like "log my breakfast" or
"how many steps yesterday" should match.

## Install (Claude Code)

```sh
mkdir -p ~/.claude/skills
cp -r skill/healthbridge ~/.claude/skills/healthbridge
```

Claude Code will pick the skill up on next launch.

## Install (any agentskills.io-compatible client)

The directory is portable. Drop it wherever your agent looks for
skills (`~/.config/<agent>/skills/`, an MCP skill directory, etc.).
The `SKILL.md` frontmatter validates against the agentskills.io
specification.

## Validate

```sh
# from https://github.com/agentskills/agentskills
skills-ref validate ./skill/healthbridge
```

## Updating

When the CLI grows a new sample type or command:

1. `cd cli && go build ./...`
2. Run `healthbridge types --json` and update `references/TYPES.md`.
3. Run `healthbridge <new-command> --help` and update
   `references/COMMANDS.md`.
4. Bump `metadata.version` in `SKILL.md`.
