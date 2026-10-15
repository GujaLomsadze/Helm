// Package all registers every agent adapter TRK ships. Import it for its side effect.
package all

import (
	_ "github.com/GujaLomsadze/trk/internal/agent/claude"
	_ "github.com/GujaLomsadze/trk/internal/agent/codex"
)
