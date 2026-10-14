// Package web embeds the dashboard (no build step).
package web

import "embed"

//go:embed index.html app.js editor.js theme.css limits.html limits.js codex-limits.js limits.css stats.html stats.js stats.css term.html term.js agents.js vendor fonts icons
var FS embed.FS
