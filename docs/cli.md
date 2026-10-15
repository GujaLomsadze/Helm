# CLI and configuration

## Commands

| Command | What it does |
|---|---|
| `hlm init [--agent codex] [--dry-run]` | Connect an agent: hooks, status line, instruction-file block. See [Claude Code](claude-code.md) and [Codex](codex.md) |
| `hlm open` | Open the dashboard (starts Helm if it's stopped) |
| `hlm stop` | Stop Helm; hooks pause until `hlm open` |
| `hlm update [--check]` | Install the latest release |
| `hlm serve` | Run the daemon in the foreground (never needed: the first `hlm` call starts it) |
| `hlm version` | Print the version |

## Stopping Helm

```sh
hlm stop     # or the ⏻ button in the dashboard header
```

This stops the service and pauses the hooks: Claude keeps working exactly as before, `hlm` calls from agents and the status line quietly do nothing, and nothing is recorded. Run `hlm open` to start again.

## Updating

```sh
hlm update           # download the latest release, verify its checksum, swap the binary, restart the daemon
hlm update --check   # only tell me if there's a newer version
```

`hlm open` also checks once a day and offers the update when one is out. Built from source? Use `git pull && make install`.

## Agent verbs

Agents report their own progress with these (the block `hlm init` adds to CLAUDE.md / AGENTS.md tells them how):

| Command | Meaning |
|---|---|
| `hlm start "<task>" --steps N` | Declare a task |
| `hlm step "<what>"` | Declare the current step |
| `hlm progress i/N` | Declare progress |
| `hlm blocked "<question>"` | Ask the human for a decision (shows in **Needs you**) |
| `hlm done "<result>"` | Declare completion |

Every reporting call uses a ~200 ms timeout and **always exits 0**. If Helm is down, agents don't notice.

Anything that can POST JSON can report too (Codex, Aider, scripts): `POST /v1/events` with `{"source":"api","kind":"step","session_id":"my-agent","payload":{"text":"..."}}`.

## Configuration

All optional:

| Variable | Default | Purpose |
|---|---|---|
| `HLM_PORT` | `7777` | Daemon port (binds 127.0.0.1 only) |
| `HLM_URL` | — | Full daemon URL override, e.g. to reach a daemon on the other side of WSL. Disables auto-spawn. |
| `HLM_DATA_DIR` | `~/.local/share/hlm` · `~/Library/Application Support/hlm` · `%APPDATA%\hlm` | SQLite event log + daemon log |
| `HLM_SESSION` | — | Force which session CLI calls are attributed to |
| `HLM_MAX_DB_MB` | `5120` | Database size cap. Past it, the oldest raw events are trimmed; session summaries stay |
| `HLM_NO_UPDATE_CHECK` | — | Set to anything to stop `hlm open` from checking GitHub for updates |
| `HLM_CODEX_COMMAND` | `codex` on `PATH` | The Codex executable Helm starts and reads plan usage from |
| `CODEX_HOME` | `~/.codex` | Where `hlm init --agent codex` puts hooks.json and AGENTS.md (Codex's own variable) |

Every `HLM_*` variable is also read under its old `TRK_*` name.

## WSL

Run `hlm` inside WSL next to Claude Code. WSL2 forwards localhost, so open `http://localhost:7777` in your Windows browser. For agents in both WSL and native Windows, run one daemon and point the other side at it with `HLM_URL` (mirrored networking mode makes this seamless).

---
[← Back to the README](../README.md)
