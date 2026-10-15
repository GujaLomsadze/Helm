# Helm

A local, single-binary tracker that shows, live, what every coding agent on your machine is doing: progress, status, context usage, plan limits, and which agents need a human. Agents report through a tiny CLI; Claude Code and Codex hooks report automatically. A browser dashboard (phosphor CRT theme) shows the whole fleet.

Helm shows two things side by side: **declared intent** (`hlm step "writing tests"`) and **ground truth** (every tool call, permission prompt and context % that Claude Code reports through hooks). The gap between them is the useful part, e.g. *"says step 3/5 writing tests; reality: pytest failed 14× in 6 min"*.

> **Helm was called TRK** (command `trk`). Coming from TRK? `trk update` brings you here, then run `hlm init` once: it moves your hooks, status line and CLAUDE.md block over to `hlm`. Your history moves to Helm's data folder on its own, and `trk` keeps working as an alias for now. Old `TRK_*` environment variables are still read. On TRK v0.2.0, `trk update` can't follow the repo's rename (it says HTTP 301): run the install command below once instead.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/GujaLomsadze/helm/main/install.sh | sh
```

Or `go install github.com/GujaLomsadze/helm/cmd/hlm@latest`.

Binaries for Linux, macOS and Windows on amd64 and arm64 are on the [releases page](https://github.com/GujaLomsadze/helm/releases).

## Quick start

```sh
hlm init --dry-run   # preview exactly what would change (writes nothing)
hlm init     # add hooks + status line to ~/.claude/settings.json, add a CLAUDE.md block
             # (start or restart Claude Code)
hlm open     # open http://localhost:7777
```

Using Codex too? `hlm init --agent codex` (see [Codex](#codex)). `hlm init` tells you when it finds Codex installed.

Worried about your Claude setup? Read [How Helm works with Claude Code](#how-hlm-works-with-claude-code-and-why-it-wont-break-it): it's observe-only, async, backed up and reversible.

You never need to run `hlm serve` yourself: the first `hlm` call that finds no daemon starts one in the background.

## Codex

```sh
hlm init --agent codex --dry-run   # preview
hlm init --agent codex             # hooks in ~/.codex/hooks.json, progress block in ~/.codex/AGENTS.md
```

Then restart Codex and review the Helm hooks in its `/hooks` screen: Codex asks you to trust new hooks, and Helm never edits `config.toml` or trust settings for you. `CODEX_HOME` moves both files; `--instructions PATH` picks another AGENTS.md and `--no-instructions` skips it.

What you get: a card per Codex session with its prompts, replies, tool calls (patches count every file they touch), permission prompts, interrupts and subagents, and `hlm step` progress (Codex gives its commands their thread id, so no setup is needed). Collisions work across agents: a Claude edit and a Codex patch to one file show up together.

What Codex doesn't report: cost and tokens per session, so its cards show `cost —` instead of a made-up `$0.00`. Your Codex plan usage (5-hour and weekly) is on the Limits page instead, read on demand from the Codex CLI you're signed in with.

Once a second agent is in use, cards show whose they are (a small glyph), and Fleet and Stats get an agent filter. With only Claude Code, nothing changes.

## The dashboard

Three pages, all at `http://localhost:7777`:

- **Fleet:** one card per agent with its task, progress, status, context gauge, last prompt and reply, files touched and more. Cards glow by status (a pulsing amber card is waiting on you). Click a card to open its [agent drawer](#agents-in-the-dashboard). In the drawer, **✎** gives the card a title of your own. Titles are stored by Helm, so every browser shows them. **⚙ Card layout** picks the widgets, footer stats, grid size and which areas to show (Needs you and Collisions start hidden; the cards' chips already show both).
- **Limits:** your 5-hour and weekly plan usage (Claude Code, and Codex below it when you use it), one full-width chart each, with an "at this pace" projection. The verdict says **Perfect pace** within ±5% of the safe pace, **Good pace** with headroom, and **Slow down** only when you'd run out before the reset.
- **Stats:** history for today, 7 or 30 days, filterable by repo (and by agent, once you use more than one). It shows spend per day by repo; where session time goes (model working, tools running, waiting on you); how long prompts wait for you; a weekday × hour activity grid; top tools with failures; the slowest commands; the most-touched files; how full each session's context got; and a sortable sessions table.

## Agents in the dashboard

Helm can also run Claude Code or Codex for you, in a terminal inside the dashboard. It's off until you turn it on in **⚙ Card layout → Terminals** (not available on native Windows; WSL works).

- **+ Agent** opens the New agent form: which agent (when more than one is installed), a folder, an optional name for its card, and an optional first prompt. The agent runs in a terminal owned by Helm; Codex runs with `--no-daemon`, so stopping it never touches Codex sessions Helm didn't start. Closing the drawer keeps it running; **Pop out ↗** moves it to its own window; **A− / A+** sets the terminal's font size.
- **Click any card** to open its drawer:
  - running in Helm: its terminal.
  - running in another terminal (a tab, your editor): **Fork into Helm** continues a copy of the conversation (`claude --resume <id> --fork-session`) and leaves the original alone. **Take over** stays off while that claude runs, so two claudes never write to one conversation.
  - not running: **Resume** continues it in Helm (`claude --resume <id>`, `codex resume <id>`).
  - always: **New agent here** (fresh claude in the same folder) and **Copy resume command**.
- Agents started in Helm stop when Helm stops, restarts or updates (`hlm stop` and `hlm update` say how many). Their conversations are saved: resume them from their cards.

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

| Command | Meaning |
|---|---|
| `hlm start "<task>" --steps N` | Declare a task |
| `hlm step "<what>"` | Declare the current step |
| `hlm progress i/N` | Declare progress |
| `hlm blocked "<question>"` | Ask the human for a decision (shows in **Needs you**) |
| `hlm done "<result>"` | Declare completion |

Every reporting call uses a ~200 ms timeout and **always exits 0**. If Helm is down, agents don't notice.

Anything that can POST JSON can report too (Codex, Aider, scripts): `POST /v1/events` with `{"source":"api","kind":"step","session_id":"my-agent","payload":{"text":"..."}}`.

## Context bands

Each card tracks the session's context window usage:

| Usage | Gauge | Hint on the card |
|---|---|---|
| < 40% | normal | — |
| ≥ 40% | yellow | — |
| ≥ 50% | yellow | Start thinking about handing off / compacting |
| ≥ 60% | red | Start thinking about handing off / compacting |
| ≥ 70% | red | …plus: Don't trust architectural reasoning without re-grounding it |

## Statuses

`WAITING ON YOU` (permission prompt pending) · `BLOCKED` (agent ran `hlm blocked`) · `DONE` (turn or task finished) · `IDLE` (nothing for 2 min) · `LOOPING?` (same command failing ≥5× in 10 min, or one file edited ≥8× with no progress report) · `WORKING`.

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

## How Helm works with Claude Code (and why it won't break it)

Helm **watches** Claude Code. It doesn't drive it, and it can't change what Claude does.

```
 Claude Code ──hook event──▶ hlm hook ─┐
     │                                  │  one local HTTP POST,
     ├─status line JSON──▶ hlm statusline ─┤  200 ms timeout,
     │                                  │  always exits 0
     └─runs──▶ hlm step "…" (agent) ────┘
                                        ▼
                         hlm daemon (127.0.0.1:7777) ──▶ SQLite in your data dir
                                        │
                                        └──SSE──▶ dashboard in your browser
```

### What `hlm init` changes

Exactly three things, all reversible:

1. **Hooks** in `~/.claude/settings.json`: one `hlm hook` entry for each of `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`, `Notification`, `Stop`, `SubagentStop`, `PreCompact`. They're added alongside your existing hooks. Nothing of yours is removed or reordered.
2. **Status line.** If you have none, it becomes `hlm statusline`. If you already have one (e.g. `ccstatusline`), Helm asks before chaining it: `hlm statusline --then '<your command>'`. Your command gets the same input, and its output is printed **unchanged**. Other settings like `padding` and `refreshInterval` are kept.
3. **CLAUDE.md:** a short block between `<!-- hlm:begin -->` and `<!-- hlm:end -->` that asks agents to report progress with `hlm start/step/progress/blocked/done`.

Before writing, Helm saves each original as `<file>.hlm-backup-YYYYMMDD-HHMMSS`. Running `hlm init` again changes nothing. To see the exact changes first: `hlm init --dry-run`. To skip parts: `--no-statusline`, `--no-claude-md`.

### Why it can't slow down, block or break Claude

| Worry | What actually happens |
|---|---|
| "Hooks will slow every tool call" | Hooks are registered with `"async": true`, so Claude starts them and moves on without waiting. |
| "A hook could approve or deny things" | Async hook output is discarded by Claude Code, and `hlm hook` prints nothing anyway. Helm cannot approve, deny, block or inject anything into Claude's context. |
| "If Helm crashes, Claude breaks" | Every `hlm` call gives up after ~200 ms and **always exits 0**. Daemon down → silent no-op. The next call restarts it. |
| "It messes up my status line" | A chained status line prints your command's output byte for byte. Helm only reads the JSON on the way through. |
| "The dashboard can start programs" | Only if you turn terminals on, and then only `claude` in a folder you pick, through the same localhost + Origin/Host guard as everything else; a web page can't switch them on for itself (the setting lives in the daemon). |
| "It phones home" | The daemon and hooks never go online: the daemon binds `127.0.0.1` only, and requests from web pages (foreign `Origin`/`Host`) are refused. The only outbound request is `hlm open` asking GitHub, at most once a day, whether a newer release exists. Turn it off with `HLM_NO_UPDATE_CHECK=1`. |
| "It rewrites my settings badly" | Invalid JSON → Helm stops and writes nothing. Key order, number formatting, file permissions and symlinked dotfiles are preserved. |

### Good to know

- **What gets stored.** Hook payloads go into a local SQLite file in your data dir (`~/.local/share/hlm/` on Linux). They can include your prompts, shell commands, file paths and snippets of file contents (large values are trimmed). The Stats page is computed from this file. Nothing leaves your machine. Delete the folder any time to wipe it.
- **Agents calling `hlm`.** Because of the CLAUDE.md block, agents run `hlm …` through their Bash tool, which may trigger a permission prompt the first time. To allow it once and for all, add this to `~/.claude/settings.json`:
  ```json
  "permissions": { "allow": ["Bash(hlm:*)"] }
  ```
  Each call costs the agent a few tokens.
- **Uninstall in the right order.** If you delete the `hlm` binary but leave the hooks in place, Claude Code will report hook errors and a chained status line will go blank. Undo first (below), then remove the binary.

### Undo

```sh
# 1. put the originals back (pick the newest backup of each)
cp ~/.claude/settings.json.hlm-backup-<timestamp> ~/.claude/settings.json
cp ~/.claude/CLAUDE.md.hlm-backup-<timestamp>     ~/.claude/CLAUDE.md
# 2. stop the daemon and remove hlm
kill "$(cat ~/.local/share/hlm/hlm.pid)"
rm -r ~/.local/bin/hlm ~/.local/share/hlm
```

Edited the files since? Undo by hand instead: delete the hook entries whose command ends in `hlm hook`, change `statusLine.command` back to the part after `--then` (without the quotes), and delete the `hlm:begin`…`hlm:end` block from CLAUDE.md.

## WSL

Run `hlm` inside WSL next to Claude Code. WSL2 forwards localhost, so open `http://localhost:7777` in your Windows browser. For agents in both WSL and native Windows, run one daemon and point the other side at it with `HLM_URL` (mirrored networking mode makes this seamless).

## Development

Requires Go (see `go.mod`). Everything runs through `make`; `make help` lists all targets. Dev targets use port **17777** and `/tmp/hlm-dev`, so they never touch a real Helm on :7777 or your real `~/.claude`.

```sh
make demo          # build, start a dev daemon in the background, seed a fake fleet
                   # → http://localhost:17777
make stop          # stop it
make run           # dev daemon in the foreground instead (Ctrl+C to stop)
make seed          # post the demo events again
make logs          # tail the dev daemon log

make test          # unit tests
make e2e           # end-to-end acceptance test (Linux/macOS)
make check         # vet + race tests + e2e + gofmt, what CI runs

make init-sandbox  # run `hlm init` on a copy of your settings in /tmp, show the result
make init-dry      # what `hlm init` would change in your real ~/.claude (writes nothing)
make install       # build and install to ~/.local/bin, then run `hlm init` yourself

make cross         # build all 6 release targets into ./bin
make snapshot      # GoReleaser dry run into ./dist
make clean         # remove bin/, dist/ and dev data
```

The dashboard (`web/`) is embedded in the binary, so restart the daemon after changing it (`make stop demo`).
After a visual change, open `/ui.html` on the dev daemon: every colour token (with its contrast) and every component, from fixtures. Colours belong in `theme.css`'s `:root`; a test keeps them there.
To try dashboard terminals without starting real agents, run the dev daemon with `HLM_AGENT_COMMAND=bash`: terminals then run that program instead of the agent.

Each agent is an adapter in `internal/agent/<name>` (hooks → Helm's event schema, capabilities, installer, launch). The rest of Helm never names an agent; to add one, write an adapter, register it in `internal/agent/all` and drop its icon in `web/icons/<name>.svg`.

## License

Apache License 2.0. See [LICENSE](LICENSE).
