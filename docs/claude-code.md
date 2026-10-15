# Helm and Claude Code

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

## What `hlm init` changes

Exactly three things, all reversible:

1. **Hooks** in `~/.claude/settings.json`: one `hlm hook` entry for each of `SessionStart`, `SessionEnd`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PermissionRequest`, `Notification`, `Stop`, `SubagentStop`, `PreCompact`. They're added alongside your existing hooks. Nothing of yours is removed or reordered.
2. **Status line.** If you have none, it becomes `hlm statusline`. If you already have one (e.g. `ccstatusline`), Helm asks before chaining it: `hlm statusline --then '<your command>'`. Your command gets the same input, and its output is printed **unchanged**. Other settings like `padding` and `refreshInterval` are kept.
3. **CLAUDE.md:** a short block between `<!-- hlm:begin -->` and `<!-- hlm:end -->` that asks agents to report progress with `hlm start/step/progress/blocked/done`.

Before writing, Helm saves each original as `<file>.hlm-backup-YYYYMMDD-HHMMSS`. Running `hlm init` again changes nothing. To see the exact changes first: `hlm init --dry-run`. To skip parts: `--no-statusline`, `--no-claude-md`.

## Why it can't slow down, block or break Claude

| Worry | What actually happens |
|---|---|
| "Hooks will slow every tool call" | Hooks are registered with `"async": true`, so Claude starts them and moves on without waiting. |
| "A hook could approve or deny things" | Async hook output is discarded by Claude Code, and `hlm hook` prints nothing anyway. Helm cannot approve, deny, block or inject anything into Claude's context. |
| "If Helm crashes, Claude breaks" | Every `hlm` call gives up after ~200 ms and **always exits 0**. Daemon down → silent no-op. The next call restarts it. |
| "It messes up my status line" | A chained status line prints your command's output byte for byte. Helm only reads the JSON on the way through. |
| "The dashboard can start programs" | Only if you turn terminals on, and then only `claude` in a folder you pick, through the same localhost + Origin/Host guard as everything else; a web page can't switch them on for itself (the setting lives in the daemon). |
| "It phones home" | The daemon and hooks never go online: the daemon binds `127.0.0.1` only, and requests from web pages (foreign `Origin`/`Host`) are refused. The only outbound request is `hlm open` asking GitHub, at most once a day, whether a newer release exists. Turn it off with `HLM_NO_UPDATE_CHECK=1`. |
| "It rewrites my settings badly" | Invalid JSON → Helm stops and writes nothing. Key order, number formatting, file permissions and symlinked dotfiles are preserved. |

## Good to know

- **What gets stored.** Hook payloads go into a local SQLite file in your data dir (`~/.local/share/hlm/` on Linux). They can include your prompts, shell commands, file paths and snippets of file contents (large values are trimmed). The Stats page is computed from this file. Nothing leaves your machine. Delete the folder any time to wipe it.
- **Agents calling `hlm`.** Because of the CLAUDE.md block, agents run `hlm …` through their Bash tool, which may trigger a permission prompt the first time. To allow it once and for all, add this to `~/.claude/settings.json`:
  ```json
  "permissions": { "allow": ["Bash(hlm:*)"] }
  ```
  Each call costs the agent a few tokens.
- **Uninstall in the right order.** If you delete the `hlm` binary but leave the hooks in place, Claude Code will report hook errors and a chained status line will go blank. Undo first (below), then remove the binary.

## Undo

```sh
# 1. put the originals back (pick the newest backup of each)
cp ~/.claude/settings.json.hlm-backup-<timestamp> ~/.claude/settings.json
cp ~/.claude/CLAUDE.md.hlm-backup-<timestamp>     ~/.claude/CLAUDE.md
# 2. stop the daemon and remove hlm
kill "$(cat ~/.local/share/hlm/hlm.pid)"
rm -r ~/.local/bin/hlm ~/.local/share/hlm
```

Edited the files since? Undo by hand instead: delete the hook entries whose command ends in `hlm hook`, change `statusLine.command` back to the part after `--then` (without the quotes), and delete the `hlm:begin`…`hlm:end` block from CLAUDE.md.

---
[← Back to the README](../README.md)
