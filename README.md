# TRK

A local, single-binary tracker that shows, live, what every coding agent on your machine is doing: progress, status, context usage, plan limits, and which agents need a human. Agents report through a tiny CLI; Claude Code hooks and the status line report automatically. A browser dashboard (phosphor CRT theme) shows the whole fleet.

TRK shows two things side by side: **declared intent** (`trk step "writing tests"`) and **ground truth** (every tool call, permission prompt and context % that Claude Code reports through hooks). The gap between them is the useful part, e.g. *"says step 3/5 writing tests; reality: pytest failed 14× in 6 min"*.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/GujaLomsadze/trk/main/install.sh | sh
```

Or `go install github.com/GujaLomsadze/trk/cmd/trk@latest`.

Binaries for Linux, macOS and Windows on amd64 and arm64 are on the [releases page](https://github.com/GujaLomsadze/trk/releases).

## Quick start

### Windows desktop app

Double-click `dist/desktop/win-unpacked/TRK.exe` to open TRK in its own window.
It starts the bundled tracker automatically. Keep the entire app folder together,
or use `dist/desktop/TRK Setup 0.1.0.exe` to install it with Desktop and Start menu
shortcuts. Closing the window keeps the tracker and running agents alive.
See [desktop build instructions](desktop/README.md) to produce these files.

#### Build or update the Windows installer

Install Node.js 22.12 or newer and the Go version specified in `go.mod`.
For the first build, run these PowerShell commands from the repository root:

```powershell
cd desktop
npm ci
npm run dist:win
```

After changing Go code, dashboard files, or Electron code, run `npm run dist:win`
again from `desktop/`. It recompiles the Go backend, bundles the dashboard, and
generates `dist/desktop/TRK Setup 0.1.0.exe` relative to the repository root.
No separate `go build` is needed. Run `npm ci` again if dependency files change.

To apply the update:

1. Use the dashboard's power button to stop TRK. This also stops agents launched by TRK.
2. Close the desktop app; closing its window alone does not stop the backend.
3. Run the newly generated installer.
4. Launch TRK from its Desktop or Start menu shortcut to use the updated backend.

### Command line

```sh
trk init --dry-run   # preview exactly what would change (writes nothing)
trk init     # add hooks + status line to ~/.claude/settings.json, add a CLAUDE.md block
             # (start or restart Claude Code)
trk open     # open http://localhost:7777
```

Worried about your Claude setup? Read [How TRK works with Claude Code](#how-trk-works-with-claude-code-and-why-it-wont-break-it): it's observe-only, async, backed up and reversible.

You never need to run `trk serve` yourself: the first `trk` call that finds no daemon starts one in the background.

### Codex

This checkout also supports passive tracking through Codex hooks (verified CLI surface: 0.162.1). Build this checkout to use the integration:

```sh
go build -o bin/trk ./cmd/trk  # on Windows: -o bin/trk.exe
bin/trk init --agent codex --dry-run
bin/trk init --agent codex
# Restart Codex and review/trust the TRK entries in /hooks.
bin/trk open
```

The installer merges `$CODEX_HOME/hooks.json` (default `~/.codex/hooks.json`), preserves other handlers, and backs up changed files. It leaves Codex trust and `config.toml` to Codex. To add optional progress instructions to a chosen file, pass `--agents-md /path/to/AGENTS.md`; no instruction file is edited by default.

Codex cards report prompts, replies, tool activity, permission waits, interruptions, and patch files alongside Claude cards. Fleet cards and Stats have provider filters. Session context, tokens, and cost are unavailable from this hook adapter; missing readings appear as unavailable, and mixed totals are partial. Codex sessions launched outside TRK remain externally owned; their drawer offers a resume command to copy. Use **+ Agent → Codex** to start an embedded terminal.

The **Limits** page shows Codex account quota usage and reset times above the Claude charts, with both accounts visible together. TRK uses a short-lived App Server connection and the read-only `account/rateLimits/read` method, cached for one minute while viewed. It displays the actual returned windows and marks missing/stale readings; Codex usage history and pace projections are not yet collected. The **Claude Code** section retains the existing charts. If the CLI is not found, set `TRK_CODEX_COMMAND` to its native executable and restart TRK. No additional hook trust or API key is needed when Codex is already signed in with ChatGPT.

For declared progress, use `trk codex start "task" --steps 3`, `trk codex step "testing"`, `trk codex progress 2/3`, `trk codex blocked "question"`, and `trk codex done "result"`. `TRK_SESSION` selects an explicit native ID; otherwise `CODEX_THREAD_ID` is used when present. Directory matching is best effort and does not choose between multiple matching Codex sessions.

See the [implementation plan and validation record](docs/codex-support-plan.md) and [project guide](PROJECT_GUIDE.md). Local desktop hook delivery has now been observed. Codex's automatic next-message suggestion forks are excluded from cards and statistics using their specific app-generated prompt preamble; real user chats and forks remain separate. Their raw history is preserved. Broader lifecycle, IDE, and cloud coverage still need separate validation.

## The dashboard

Four pages, all at `http://localhost:7777`:

- **Fleet:** one card per agent with its task, progress, status, context gauge, last prompt and reply, files touched and more. Cards glow by status (a pulsing amber card is waiting on you). Click a card to open its [agent drawer](#agents-in-the-dashboard). In the drawer, **✎** gives the card a title of your own. Titles are stored by TRK, so every browser shows them. **⚙ Card layout** picks the widgets, footer stats, grid size and which areas to show (Needs you and Collisions start hidden; the cards' chips already show both).
- **Limits:** your 5-hour and weekly plan usage, one full-width chart each, with an "at this pace" projection. The verdict says **Perfect pace** within ±5% of the safe pace, **Good pace** with headroom, and **Slow down** only when you'd run out before the reset.
- **Stats:** history for today, 7 or 30 days, filterable by repo. It shows spend per day by repo; where session time goes (model working, tools running, waiting on you); how long prompts wait for you; a weekday × hour activity grid; top tools with failures; the slowest commands; the most-touched files; how full each session's context got; and a sortable sessions table.
- **Schedules:** one-time and recurring Claude/Codex tasks, with pause/resume, manual runs, time limits, and persistent run history and output.

## Task scheduling

Open **Schedules → + Schedule task**, choose Claude Code or Codex, enter an
absolute project folder and instructions, and choose the first run time. Tasks
can run once, every interval, daily, or weekly. Enable **Fleet → ⚙ Card layout →
Terminals** to allow execution. Each run starts a fresh batch session using
`claude --print` or `codex --no-daemon exec`, with the CLI's existing sign-in and
permission settings. TRK does not automatically approve permissions or hook trust.

Schedules are stored in the TRK database and keep running when the desktop window
is closed. The tracker must remain running and the computer awake; scheduling
does not wake the computer or start TRK at Windows sign-in. If TRK was unavailable,
missed occurrences are combined into one run when it returns. A task never
overlaps its own running instance; tasks wait when all agent slots are occupied.
The default time limit is 60 minutes and can be changed per task.

Use **Pause/Resume** to control future occurrences; pausing leaves the current
run alone. **Run now** adds an extra run without changing the next scheduled
occurrence. **Stop run** ends the current run. **History** shows the last 30 runs,
their exit status, and the last 64 KiB of captured terminal output per run.
Successful means the CLI exited with code zero; review its output for the task's
actual result. Running tasks interrupted by a tracker restart are recorded as
interrupted and are not automatically retried. Edit a completed one-time task
with a future date to schedule it again, or use Run now.

Daily and weekly schedules retain the timezone of the browser that creates them
and keep the same local time across daylight saving changes. A nonexistent
spring-forward time is skipped; a repeated autumn time runs once. Interval
schedules use elapsed minutes. The First run input is shown in the browser's
current timezone; the form also displays the timezone used for recurrence.

The local API exposes `GET/POST /v1/schedules`, `PUT/DELETE /v1/schedules/{id}`,
`POST /v1/schedules/{id}/enabled` with `{"enabled": false}` or `true`,
`POST /v1/schedules/{id}/run`, `POST /v1/schedules/{id}/stop`, and
`GET /v1/schedules/{id}/runs`. Creation and edits take `name`, `provider`, `dir`,
`prompt`, `repeat` (`once`, `interval`, `daily`, `weekly`), `run_at` (Unix
milliseconds), `every_minutes` (interval only), `timezone` (IANA name), and
`timeout_minutes`. All routes use the same local Host/Origin checks as the dashboard.

## Agents in the dashboard

TRK can run Claude or Codex in a terminal inside the dashboard. Enable **⚙ Card layout → Terminals** once. Unix/WSL uses PTYs; Windows 10/11 uses ConPTY and requires native agent executables.

- **+ Agent** opens the New agent form: choose **Claude Code** or **Codex**, a folder, an optional card name, and an optional first prompt. The selected agent runs in a terminal owned by TRK. Codex uses standalone mode (`--no-daemon`), preserving normal approval and hook-trust prompts; it never takes over the shared Codex daemon. Closing the drawer keeps it running; **Pop out ↗** moves it to its own window; **A− / A+** sets the terminal's font size.
- **Click any card** to open its drawer:
  - running in TRK: its terminal.
  - running in another terminal (a tab, your editor): **Fork into TRK** continues a copy of the conversation (`claude --resume <id> --fork-session`) and leaves the original alone. **Take over** stays off while that claude runs, so two claudes never write to one conversation.
  - not running: **Resume** continues it in TRK (`claude --resume <id>`).
  - always: **New agent here** (fresh claude in the same folder) and **Copy resume command**.
- Agents started in TRK stop when TRK stops, restarts or updates (`trk stop` and `trk update` say how many). Their conversations are saved: resume them from their cards.

## Stopping TRK

```sh
trk stop     # or the ⏻ button in the dashboard header
```

This stops the service and pauses the hooks: Claude keeps working exactly as before, `trk` calls from agents and the status line quietly do nothing, and nothing is recorded. Run `trk open` to start again.

## Updating

```sh
trk update           # download the latest release, verify its checksum, swap the binary, restart the daemon
trk update --check   # only tell me if there's a newer version
```

`trk open` also checks once a day and offers the update when one is out. Built from source? Use `git pull && make install`.

## Agent verbs

| Command | Meaning |
|---|---|
| `trk start "<task>" --steps N` | Declare a task |
| `trk step "<what>"` | Declare the current step |
| `trk progress i/N` | Declare progress |
| `trk blocked "<question>"` | Ask the human for a decision (shows in **Needs you**) |
| `trk done "<result>"` | Declare completion |

Every reporting call uses a ~200 ms timeout and **always exits 0**. If TRK is down, agents don't notice.

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

`WAITING ON YOU` (permission prompt pending) · `BLOCKED` (agent ran `trk blocked`) · `DONE` (turn or task finished) · `IDLE` (nothing for 2 min) · `LOOPING?` (same command failing ≥5× in 10 min, or one file edited ≥8× with no progress report) · `WORKING`.

## Configuration

All optional:

| Variable | Default | Purpose |
|---|---|---|
| `TRK_PORT` | `7777` | Daemon port (binds 127.0.0.1 only) |
| `TRK_URL` | — | Full daemon URL override, e.g. to reach a daemon on the other side of WSL. Disables auto-spawn. |
| `TRK_DATA_DIR` | `~/.local/share/trk` · `~/Library/Application Support/trk` · `%APPDATA%\trk` | SQLite event log + daemon log |
| `TRK_SESSION` | — | Force which session CLI calls are attributed to |
| `TRK_MAX_DB_MB` | `5120` | Database size cap. Past it, the oldest raw events are trimmed; session summaries stay |
| `TRK_NO_UPDATE_CHECK` | — | Set to anything to stop `trk open` from checking GitHub for updates |

## How TRK works with Claude Code (and why it won't break it)

TRK **watches** Claude Code. It doesn't drive it, and it can't change what Claude does.

```
 Claude Code ──hook event──▶ trk hook ─┐
     │                                  │  one local HTTP POST,
     ├─status line JSON──▶ trk statusline ─┤  200 ms timeout,
     │                                  │  always exits 0
     └─runs──▶ trk step "…" (agent) ────┘
                                        ▼
                         trk daemon (127.0.0.1:7777) ──▶ SQLite in your data dir
                                        │
                                        └──SSE──▶ dashboard in your browser
```

### What `trk init` changes

Exactly three things, all reversible:

1. **Hooks** in `~/.claude/settings.json`: one `trk hook` entry for each of `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`, `Notification`, `Stop`, `SubagentStop`, `PreCompact`. They're added alongside your existing hooks. Nothing of yours is removed or reordered.
2. **Status line.** If you have none, it becomes `trk statusline`. If you already have one (e.g. `ccstatusline`), TRK asks before chaining it: `trk statusline --then '<your command>'`. Your command gets the same input, and its output is printed **unchanged**. Other settings like `padding` and `refreshInterval` are kept.
3. **CLAUDE.md:** a short block between `<!-- trk:begin -->` and `<!-- trk:end -->` that asks agents to report progress with `trk start/step/progress/blocked/done`.

Before writing, TRK saves each original as `<file>.trk-backup-YYYYMMDD-HHMMSS`. Running `trk init` again changes nothing. To see the exact changes first: `trk init --dry-run`. To skip parts: `--no-statusline`, `--no-claude-md`.

### Why it can't slow down, block or break Claude

| Worry | What actually happens |
|---|---|
| "Hooks will slow every tool call" | Hooks are registered with `"async": true`, so Claude starts them and moves on without waiting. |
| "A hook could approve or deny things" | Async hook output is discarded by Claude Code, and `trk hook` prints nothing anyway. TRK cannot approve, deny, block or inject anything into Claude's context. |
| "If TRK crashes, Claude breaks" | Every `trk` call gives up after ~200 ms and **always exits 0**. Daemon down → silent no-op. The next call restarts it. |
| "It messes up my status line" | A chained status line prints your command's output byte for byte. TRK only reads the JSON on the way through. |
| "The dashboard can start programs" | Only if you turn terminals on, and then only `claude` in a folder you pick, through the same localhost + Origin/Host guard as everything else; a web page can't switch them on for itself (the setting lives in the daemon). |
| "It phones home" | The daemon and hooks never go online: the daemon binds `127.0.0.1` only, and requests from web pages (foreign `Origin`/`Host`) are refused. The only outbound request is `trk open` asking GitHub, at most once a day, whether a newer release exists. Turn it off with `TRK_NO_UPDATE_CHECK=1`. |
| "It rewrites my settings badly" | Invalid JSON → TRK stops and writes nothing. Key order, number formatting, file permissions and symlinked dotfiles are preserved. |

### Good to know

- **What gets stored.** Hook payloads go into a local SQLite file in your data dir (`~/.local/share/trk/` on Linux). They can include your prompts, shell commands, file paths and snippets of file contents (large values are trimmed). The Stats page is computed from this file. Nothing leaves your machine. Delete the folder any time to wipe it.
- **Agents calling `trk`.** Because of the CLAUDE.md block, agents run `trk …` through their Bash tool, which may trigger a permission prompt the first time. To allow it once and for all, add this to `~/.claude/settings.json`:
  ```json
  "permissions": { "allow": ["Bash(trk:*)"] }
  ```
  Each call costs the agent a few tokens.
- **Uninstall in the right order.** If you delete the `trk` binary but leave the hooks in place, Claude Code will report hook errors and a chained status line will go blank. Undo first (below), then remove the binary.

### Undo

```sh
# 1. put the originals back (pick the newest backup of each)
cp ~/.claude/settings.json.trk-backup-<timestamp> ~/.claude/settings.json
cp ~/.claude/CLAUDE.md.trk-backup-<timestamp>     ~/.claude/CLAUDE.md
# 2. stop the daemon and remove trk
kill "$(cat ~/.local/share/trk/trk.pid)"
rm -r ~/.local/bin/trk ~/.local/share/trk
```

Edited the files since? Undo by hand instead: delete the hook entries whose command ends in `trk hook`, change `statusLine.command` back to the part after `--then` (without the quotes), and delete the `trk:begin`…`trk:end` block from CLAUDE.md.

## WSL

Run `trk` inside WSL next to Claude Code. WSL2 forwards localhost, so open `http://localhost:7777` in your Windows browser. For agents in both WSL and native Windows, run one daemon and point the other side at it with `TRK_URL` (mirrored networking mode makes this seamless).

## Development

Requires Go (see `go.mod`). Everything runs through `make`; `make help` lists all targets. Dev targets use port **17777** and `/tmp/trk-dev`, so they never touch a real TRK on :7777 or your real `~/.claude`.

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

make init-sandbox  # run `trk init` on a copy of your settings in /tmp, show the result
make init-dry      # what `trk init` would change in your real ~/.claude (writes nothing)
make install       # build and install to ~/.local/bin, then run `trk init` yourself

make cross         # build all 6 release targets into ./bin
make snapshot      # GoReleaser dry run into ./dist
make clean         # remove bin/, dist/ and dev data
```

The dashboard (`web/`) is embedded in the binary, so restart the daemon after changing it (`make stop demo`).
To try dashboard terminals without starting real agents, run the dev daemon with `TRK_AGENT_COMMAND=bash`: terminals then run that program instead of `claude`.

## License

Apache License 2.0. See [LICENSE](LICENSE).
