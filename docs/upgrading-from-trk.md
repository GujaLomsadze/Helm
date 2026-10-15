# Upgrading from TRK

Helm used to be called TRK, and its command was `trk`. Everything carries over: your session history, card titles, dashboard layout and settings.

## TL;DR

```sh
curl -fsSL https://raw.githubusercontent.com/GujaLomsadze/helm/main/install.sh | sh
hlm init                 # moves your hooks, status line and CLAUDE.md block to hlm
trk stop && hlm open     # restart on the new version; your data moves over by itself
```

Using Codex with TRK? Also run `hlm init --agent codex`.

## What happens

- **The installer** puts `hlm` next to your old `trk` and turns `trk` into a link to `hlm`, so anything still calling `trk` runs Helm.
- **`hlm init`** rewrites TRK's hooks, status line (keeping anything it chained, like ccstatusline) and CLAUDE.md block to use `hlm`. It backs each file up first, like always.
- **The first Helm daemon** moves your data folder (`~/.local/share/trk` → `~/.local/share/hlm`, and the same on macOS and Windows) and renames the database inside. The dashboard keeps your layout and settings.
- **Old names keep working** for now: the `trk` command, `TRK_*` environment variables, and hooks you haven't switched yet.

## Why not `trk update`?

From TRK 0.3.0 on, `trk update` works. TRK **v0.2.0** can't follow GitHub's redirect after the repository was renamed and says `no release found (HTTP 301)`. Run the install command once instead; updates work normally after that (`hlm update`).

## Agents running in the dashboard

Agents you started in the dashboard's terminals stop when the daemon restarts (`trk stop`). Their conversations are saved: resume them from their cards.


---
[← Back to the README](../README.md)
