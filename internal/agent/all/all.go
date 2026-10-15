// Package all registers every agent adapter Helm ships. Import it for its side effect.
package all

import (
	_ "github.com/GujaLomsadze/helm/internal/agent/claude"
	_ "github.com/GujaLomsadze/helm/internal/agent/codex"
)
