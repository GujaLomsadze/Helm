package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPresetsAreWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range (Adapter{}).Presets() {
		if p.ID == "" || p.Label == "" || ids[p.ID] {
			t.Fatalf("bad preset %+v", p)
		}
		ids[p.ID] = true
		urls := 0
		for _, f := range p.Fields {
			if f.Var == "" || f.Label == "" {
				t.Fatalf("%s: bad field %+v", p.ID, f)
			}
			if f.URL {
				urls++
			}
		}
		if p.Spend != "" && urls != 1 {
			t.Fatalf("%s reads gateway spend but has %d URL fields", p.ID, urls)
		}
	}
}

func TestProfileScrubAndConflicts(t *testing.T) {
	a := Adapter{}
	for k, want := range map[string]bool{"ANTHROPIC_API_KEY": true, "ANTHROPIC_BASE_URL": true, "AWS_BEARER_TOKEN_BEDROCK": true, "PATH": false, "HOME": false} {
		if a.ProfileScrub(k) != want {
			t.Errorf("scrub %s = %v", k, !want)
		}
	}
	p := filepath.Join(t.TempDir(), "settings.json")
	_ = os.WriteFile(p, []byte(`{"env":{"ANTHROPIC_BASE_URL":"x","CLAUDE_CODE_USE_BEDROCK":"1","FOO":"1"},"apiKeyHelper":"get-key"}`), 0o644)
	got := strings.Join(settingsConflicts(p), "|")
	if !strings.Contains(got, "env.ANTHROPIC_BASE_URL") || !strings.Contains(got, "env.CLAUDE_CODE_USE_BEDROCK") ||
		!strings.Contains(got, "apiKeyHelper") || strings.Contains(got, "FOO") {
		t.Fatal(got)
	}
	if settingsConflicts(filepath.Join(t.TempDir(), "none.json")) != nil {
		t.Fatal("missing file has conflicts")
	}
}
