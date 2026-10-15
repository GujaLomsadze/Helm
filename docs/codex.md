# Helm and Codex

```sh
hlm init --agent codex --dry-run   # preview
hlm init --agent codex             # hooks in ~/.codex/hooks.json, progress block in ~/.codex/AGENTS.md
```

Then restart Codex and review the Helm hooks in its `/hooks` screen: Codex asks you to trust new hooks, and Helm never edits `config.toml` or trust settings for you. `CODEX_HOME` moves both files; `--instructions PATH` picks another AGENTS.md and `--no-instructions` skips it.

What you get: a card per Codex session with its prompts, replies, tool calls (patches count every file they touch), permission prompts, interrupts and subagents, and `hlm step` progress (Codex gives its commands their thread id, so no setup is needed). Collisions work across agents: a Claude edit and a Codex patch to one file show up together.

What Codex doesn't report: cost and tokens per session, so its cards show `cost —` instead of a made-up `$0.00`. Your Codex plan usage (5-hour and weekly) is on the Limits page instead, read on demand from the Codex CLI you're signed in with.

Once a second agent is in use, cards show whose they are (a small glyph), and Fleet and Stats get an agent filter. With only Claude Code, nothing changes.

To start Codex from the dashboard, see [Agents in the dashboard](dashboard.md#agents-in-the-dashboard).


---
[← Back to the README](../README.md)
