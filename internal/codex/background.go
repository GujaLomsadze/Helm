package codex

import (
	"strings"

	"github.com/GujaLomsadze/trk/internal/model"
)

// BackgroundSession recognizes the desktop's observed next-message suggestion
// preamble. Hooks currently provide no structured purpose/parent field for these
// forks. Match the full opening sentences, not generic words or a JSON reply.
// Use the persisted prompt so older cards are also filtered after a restart,
// without deleting their history or merging unrelated chats in the same folder.
func BackgroundSession(s model.Session) bool {
	const prefix = "This is an app-generated request for a suggested next user message. The user did not write this message."
	return s.Provider == model.ProviderCodex && strings.HasPrefix(strings.TrimSpace(s.LastPrompt), prefix)
}
