package agent_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/GujaLomsadze/helm/internal/agent"
	_ "github.com/GujaLomsadze/helm/internal/agent/all"
)

func TestSessionKeyAndSplit(t *testing.T) {
	cases := []struct{ agent, native, key string }{
		{"claude", "abc", "abc"}, // the default agent keeps the ids it always had
		{"codex", "abc", "codex:abc"},
		{"codex", "codex:abc", "codex:abc"},
		{"", "abc", "abc"},
		{"codex", "", ""},
	}
	for _, c := range cases {
		if got := agent.SessionKey(c.agent, c.native); got != c.key {
			t.Errorf("SessionKey(%q,%q) = %q", c.agent, c.native, got)
		}
	}
	if a, n := agent.Split("codex:abc"); a != "codex" || n != "abc" {
		t.Fatal(a, n)
	}
	if a, n := agent.Split("cwd-1234"); a != "" || n != "cwd-1234" {
		t.Fatal(a, n)
	}
	if a, n := agent.Split("weird:id"); a != "" || n != "weird:id" {
		t.Fatal(a, n)
	}
}

func TestRegistry(t *testing.T) {
	all := agent.All()
	if len(all) < 2 || all[0].Name() != agent.Default {
		t.Fatal("default agent first")
	}
	for _, a := range all {
		if a.Label() == "" || a.ResumeCommand("x") == "" {
			t.Errorf("%s: incomplete adapter", a.Name())
		}
	}
}

// The core reads canonical events and Caps; it never asks which agent it is looking at.
// Agent names belong in internal/agent/<name> (and the CLI's help text).
func TestCoreNeverNamesAnAgent(t *testing.T) {
	var names []string
	for _, a := range agent.All() {
		names = append(names, regexp.QuoteMeta(a.Name()))
	}
	re := regexp.MustCompile(`"(` + strings.Join(names, "|") + `)[":]`)
	for _, dir := range []string{"derive", "fleet", "stats", "store", "daemon", "term", "model"} {
		files, _ := filepath.Glob(filepath.Join("..", dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for i, line := range strings.Split(string(b), "\n") {
				if re.MatchString(line) {
					t.Errorf("%s:%d names an agent: %s", f, i+1, strings.TrimSpace(line))
				}
			}
		}
	}
}
