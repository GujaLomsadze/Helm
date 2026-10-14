# Codex hook fixtures

`lifecycle.json` contains synthetic examples of the documented Codex hook envelope,
with fake session IDs, prompts, paths, and tool output. It is not a recording of a
live authenticated conversation. Source: [OpenAI hook documentation](https://learn.chatgpt.com/docs/hooks), checked 2026-10-14.

The locally installed CLI reports 0.162.1 and `hooks stable true`. Its help confirms
resume/fork and shared-daemon operation. Automated tests replay these fixtures
through the adapter, HTTP API, SQLite, statistics, and restart reconstruction.
They do not establish real desktop/IDE/cloud event coverage.

Failure extraction recognizes structured exit codes and MCP error flags, plus
the known shell transport exit header. Unrecognized completion output remains
unknown. Patch headers are parsed before payload trimming, including move targets.
