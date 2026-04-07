# skill/healthbridge

The HealthBridge agent skill — a thin wrapper that teaches an LLM agent
how to drive the `healthbridge` CLI.

## Install (Claude Code)

```sh
mkdir -p ~/.claude/skills
cp -r skill/healthbridge ~/.claude/skills/
```

Then run `healthbridge pair` once to establish a session with your
iPhone, set `HEALTHBRIDGE_PAIR` in your shell profile, and Claude will
pick up the skill automatically when you ask anything Health-related.

## Layout

```
SKILL.md         The agent manifest the LLM reads
examples/        Sample prompts and the commands they should produce
schemas/         JSON schemas describing the CLI's --json output shapes
```

## Adding examples

Drop more `examples/<topic>.md` files. Each one should describe a user
prompt, the exact CLI command (or commands) the agent should run, and
the expected JSON shape it will get back. The skill manifest in
`SKILL.md` references the examples folder by name.
