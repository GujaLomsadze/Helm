# TRK desktop

Double-click `dist/desktop/win-unpacked/TRK.exe` to launch the desktop app. Keep
the entire `win-unpacked` folder together; it includes Electron and the Go
backend. No terminal command, Node installation, or Go installation is needed
to run the packaged app.

`dist/desktop/TRK Setup 0.1.0.exe` installs it for the current Windows user and
creates Desktop and Start menu shortcuts. The installer is unsigned in local
builds; release signing requires the project's own signing certificate.

The app connects to the existing local tracker on port 7777 or starts its bundled
backend if none is running. It shares the usual TRK database and hooks. Closing
the app leaves the tracker and managed agents running. Use the dashboard's power
button to stop them. Double-clicking the app again focuses its existing window.
Browser layout preferences are separate from Electron's persistent preferences.

## Development and packaging

From `desktop/`, with Node.js 22.12 or newer and the Go version in the root `go.mod` installed:

```powershell
npm ci
npm test
npm start
npm run dist:win
```

The build script compiles `../cmd/trk` into `resources/trk.exe` before packaging;
generated backend files, dependencies, caches and distributions are ignored.
Commit `package-lock.json` to reproduce dependency versions. `TRK_GO` can point
to a specific Go executable. `TRK_PORT` selects a different loopback port and
`TRK_DATA_DIR` selects a different tracker database; normal launches need neither.
Do not point live agent hooks at a temporary packaging or smoke-test directory.

The main process exposes no Node or filesystem bridge to dashboard scripts.
Renderers use sandboxing and context isolation. Navigation is restricted to
tracker pages on the chosen loopback origin; terminal pop-outs keep the same
restrictions. Other popup destinations and device permissions are denied.

For an automated packaged startup check, run the executable with `--smoke-test`.
It loads the dashboard in a hidden window, writes `smoke-result.json` in Electron's
user-data directory, and exits without stopping the backend. Use an isolated
port and `TRK_DATA_DIR` for testing cold starts; stop that test backend afterward.
