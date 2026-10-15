# Development

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

---
[← Back to the README](../README.md)
