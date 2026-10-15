# The dashboard

`hlm open` opens it at `http://localhost:7777`. It updates live; nothing to refresh.

Three pages, all at `http://localhost:7777`:

- **Fleet:** one card per agent with its task, progress, status, context gauge, last prompt and reply, files touched and more. Cards glow by status (a pulsing amber card is waiting on you). Click a card to open its [agent drawer](#agents-in-the-dashboard). In the drawer, **✎** gives the card a title of your own. Titles are stored by Helm, so every browser shows them. **⚙ Card layout** picks the widgets, footer stats, grid size and which areas to show (Needs you and Collisions start hidden; the cards' chips already show both).
- **Limits:** your 5-hour and weekly plan usage (Claude Code, and Codex below it when you use it), one full-width chart each, with an "at this pace" projection. The verdict says **Perfect pace** within ±5% of the safe pace, **Good pace** with headroom, and **Slow down** only when you'd run out before the reset.
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

Helm can also run Claude Code or Codex for you, in a terminal inside the dashboard. It's off until you turn it on in **⚙ Card layout → Terminals** (not available on native Windows; WSL works).

- **+ Agent** opens the New agent form: which agent (when more than one is installed), a folder, an optional name for its card, and an optional first prompt. The agent runs in a terminal owned by Helm; Codex runs with `--no-daemon`, so stopping it never touches Codex sessions Helm didn't start. Closing the drawer keeps it running; **Pop out ↗** moves it to its own window; **A− / A+** sets the terminal's font size.
- **Click any card** to open its drawer:
  - running in Helm: its terminal.
  - running in another terminal (a tab, your editor): **Fork into Helm** continues a copy of the conversation (`claude --resume <id> --fork-session`) and leaves the original alone. **Take over** stays off while that claude runs, so two claudes never write to one conversation.
  - not running: **Resume** continues it in Helm (`claude --resume <id>`, `codex resume <id>`).
  - always: **New agent here** (fresh claude in the same folder) and **Copy resume command**.
- Agents started in Helm stop when Helm stops, restarts or updates (`hlm stop` and `hlm update` say how many). Their conversations are saved: resume them from their cards.

## Several agents

With more than one agent in use (Claude Code and Codex), each card shows its agent's mark, and Fleet and Stats get an agent filter. With only Claude Code, nothing changes.


---
[← Back to the README](../README.md)
