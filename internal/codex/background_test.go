package codex

import (
	"github.com/GujaLomsadze/trk/internal/model"
	"testing"
)

func TestBackgroundSession(t *testing.T) {
	preamble := "This is an app-generated request for a suggested next user message. The user did not write this message."
	for _, c := range []struct {
		provider, prompt string
		want             bool
	}{
		{"codex", preamble + " Return a suggestion.", true},
		{"claude", preamble, false},
		{"codex", "Explain: " + preamble, false},
		{"codex", "Suggest my next message", false},
		{"codex", `{"suggestion":"a legitimate user request"}`, false},
		{"codex", "", false},
	} {
		if got := BackgroundSession(model.Session{Provider: c.provider, LastPrompt: c.prompt}); got != c.want {
			t.Fatalf("%q: %v", c.prompt, got)
		}
	}
}
