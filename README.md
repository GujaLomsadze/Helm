<h1 align="center">HELM</h1>

<p align="center">
  <b>Mission control for your coding agents.</b><br>
  See what every Claude Code and Codex session on your machine is doing, live, in one dashboard.
</p>

<p align="center">
  <a href="https://github.com/GujaLomsadze/Helm/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/GujaLomsadze/Helm?color=FF5A4A&label=release"></a>
  <a href="https://github.com/GujaLomsadze/Helm/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/GujaLomsadze/Helm/ci.yml?branch=main&label=ci"></a>
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-B9928A"></a>
</p>

<!-- Screenshot goes here: docs/img/fleet.png (Fleet page with a few agents running). -->

---

Run three agents at once and you stop knowing which one is stuck, which one is waiting on you, and which one is about to run out of context. Helm watches them for you:

- **One card per agent:** its task and progress, what it's running right now, its context window, and its cost when the agent reports one.
- **What it says vs. what it does:** an agent says *"step 3/5, writing tests"*; Helm shows *"pytest failed 14× in 6 min"* next to it.
- **Who needs you:** permission prompts and questions glow on the card, so no agent sits idle waiting for an answer.
- **Collisions:** two agents editing the same file, flagged as it happens.
- **Plan limits and history:** your 5-hour and weekly usage with an "at this pace" forecast, and a Stats page for spend, time and tools.
- **Run agents from the dashboard:** start, resume or fork Claude Code and Codex sessions in a built-in terminal.

Helm watches through the agents' own hooks: it never steers them, never slows them down, and keeps everything on your machine.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/GujaLomsadze/helm/main/install.sh | sh
```

One static binary, `hlm`, for Linux and macOS (amd64 and arm64). On Windows, download the zip from the [latest release](https://github.com/GujaLomsadze/Helm/releases/latest). With Go: `go install github.com/GujaLomsadze/helm/cmd/hlm@latest`.

## Quick start

```sh
hlm init     # connect Claude Code (backs up your settings first; --dry-run to preview)
hlm open     # open the dashboard at http://localhost:7777
```

Restart your Claude Code sessions so they pick up the hooks. That's it: cards appear as soon as an agent does something.

Using Codex? Also run `hlm init --agent codex`, then trust the Helm hooks in Codex's `/hooks` screen. [More on Codex](docs/codex.md).

To update later: `hlm update`.

> **Coming from TRK?** Helm used to be called TRK. Run the install command above once, then `hlm init`. Your history and settings come along. [Details](docs/upgrading-from-trk.md).

## Documentation

| | |
|---|---|
| [The dashboard](docs/dashboard.md) | Fleet, Limits and Stats pages, statuses, context bands, running agents from the dashboard |
| [Claude Code](docs/claude-code.md) | What `hlm init` changes, why Helm can't slow down or break Claude, privacy, undo |
| [Codex](docs/codex.md) | Connecting Codex, what it reports, plan usage |
| [CLI and configuration](docs/cli.md) | Every command, agent progress verbs, the HTTP API, environment variables, WSL |
| [Upgrading from TRK](docs/upgrading-from-trk.md) | Moving an existing TRK install to Helm |
| [Development](docs/development.md) | Building, testing, the dev daemon, how agents plug in |

## License

Apache License 2.0. See [LICENSE](LICENSE).
