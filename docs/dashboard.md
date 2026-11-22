# The dashboard

`hlm open` opens it at `http://localhost:7777`. It updates live; nothing to refresh.

Three pages, all at `http://localhost:7777`:

- **Fleet:** one card per agent with its task, progress, status, context gauge, last prompt and reply, files touched and more. Cards glow by status (a pulsing amber card is waiting on you). Click a card to open its [agent drawer](#agents-in-the-dashboard). In the drawer, **✎** gives the card a title of your own. Titles are stored by Helm, so every browser shows them. **⚙ Card layout** picks the widgets, footer stats, grid size and which areas to show (Needs you and Collisions start hidden; the cards' chips already show both).
- **Usage:** your 5-hour and weekly plan usage (Claude Code, and Codex below it when you use it), one full-width chart each, with an "at this pace" projection. The verdict says **Perfect pace** within ±5% of the safe pace, **Good pace** with headroom, and **Slow down** only when you'd run out before the reset. Below them, **API keys**: spend per [launch profile](#launch-profiles) against its budget, and a chart of spend over 7, 30 or 90 days.
- **Stats:** history for today, 7 or 30 days, filterable by repo (and by agent, once you use more than one). It shows spend per day by repo; where session time goes (model working, tools running, waiting on you); how long prompts wait for you; a weekday × hour activity grid; top tools with failures; the slowest commands; the most-touched files; how full each session's context got; and a sortable sessions table.

## Statuses

Each card carries one status chip:

| Chip | Meaning |
|---|---|
| `WAITING ON YOU` | a permission prompt or question is pending |
| `BLOCKED` | the agent ran `hlm blocked "<question>"` |
| `LOOPING?` | the same command failed ≥5× in 10 min, or one file was edited ≥8× with no progress report |
| `WORKING` | busy |
| `INTERRUPTED` | you stopped the turn (Codex) |
| `DONE` | the turn or task finished |
| `IDLE` | nothing for 2 minutes |

## Context bands

Each card tracks the session's context window usage:

| Usage | Gauge | Hint on the card |
|---|---|---|
| < 40% | normal | — |
| ≥ 40% | yellow | — |
| ≥ 50% | yellow | Start thinking about handing off / compacting |
| ≥ 60% | red | Start thinking about handing off / compacting |
| ≥ 70% | red | …plus: Don't trust architectural reasoning without re-grounding it |

## Agents in the dashboard

Helm can also run Claude Code or Codex for you, in a terminal inside the dashboard. It's on by default; turn it off in **⚙ Card layout → Terminals** (not available on native Windows; WSL works).

- **+ Agent** opens the New agent form: which agent (when more than one is installed), a folder, an optional name for its card, and an optional first prompt. Typing a path suggests matching folders from disk; **☆** pins the folder so it's offered on every start, even after a restart (pinned folders show first, marked ★; click ★ again to unpin). The agent runs in a terminal owned by Helm; Codex runs with `--no-daemon`, so stopping it never touches Codex sessions Helm didn't start. Closing the drawer keeps it running; **Pop out ↗** moves it to its own window; **A− / A+** sets the terminal's font size.
- **Click any card** to open its drawer:
  - running in Helm: its terminal.
  - running in another terminal (a tab, your editor): **Fork into Helm** continues a copy of the conversation (`claude --resume <id> --fork-session`) and leaves the original alone. **Take over** stays off while that claude runs, so two claudes never write to one conversation.
  - not running: **Resume** continues it in Helm (`claude --resume <id>`, `codex resume <id>`).
  - always: **New agent here** (fresh claude in the same folder) and **Copy resume command**.
- Agents started in Helm stop when Helm stops, restarts or updates (`hlm stop` and `hlm update` say how many). Their conversations are saved: resume them from their cards.

## Launch profiles

A launch profile starts Claude Code on a provider other than your own sign-in: your company's LLM gateway (LiteLLM, Portkey or any other), an Anthropic API key, Amazon Bedrock (API key, AWS profile or through a gateway) or Google Vertex through a gateway. **Custom** takes any environment variables you add. Nothing to install and no shell aliases: Helm sets the variables on the agents it starts.

- **Add one** from **+ Agent → + profile** or **Usage → + Add profile**: a name, the type, its URL and key, an optional budget (USD over 30, 7 or 1 days, or since created) and a colour. **Extra variables** take anything else (`ANTHROPIC_MODEL`, `ANTHROPIC_CUSTOM_HEADERS`, …).
- **Pick one** in the New agent form: one chip per profile with what's left of its budget, plus **My login** (your own sign-in). Helm remembers the last pick. Each card started with a profile shows its name; resuming or forking a session keeps its profile, so a company session never continues on your personal login.
- **Keys** stay on this machine in `secrets.json` next to Helm's database (readable only by you). The dashboard never gets them back, only whether each one is set. To use a new key, **Duplicate with a new key**.
- **Spend** is Claude Code's own cost estimate for the sessions that ran on the profile. On a LiteLLM gateway Helm can show the key's real spend and budget instead: when you save a gateway profile it asks the gateway's `/key/info` and offers to use those numbers (some gateways only let the admin key read them; then the estimate stays). The gateway's own budget wins over the one typed in Helm.
- **Budget colours:** yellow from 80%, red from 100%. Helm warns in the New agent form but still starts the agent; the gateway enforces the real limit.
- Agents get the profile's variables in place of your shell's own `ANTHROPIC_*` settings. Variables in the `env` block of `~/.claude/settings.json` (or an `apiKeyHelper` there) override any profile, because Claude Code applies them over the environment; the profile editor warns when it finds some.
- Only agents started from the dashboard use profiles. A session you start in your own terminal runs on whatever that shell has.

## Several agents

With more than one agent in use (Claude Code and Codex), each card shows its agent's mark, and Fleet and Stats get an agent filter. With only Claude Code, nothing changes.


---
[← Back to the README](../README.md)
