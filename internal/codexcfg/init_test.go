package codexcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitDryRunPreservesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	p := Paths{filepath.Join(dir, "hooks.json"), filepath.Join(dir, "AGENTS.md")}
	original := `{"description":"mine","hooks":{"Stop":[{"hooks":[{"type":"command","command":"other-tool"}]}]}}`
	if err := os.WriteFile(p.Hooks, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p.AgentsMD, []byte("# My instructions\n"), 0o600)
	command := `"C:/Program Files/trk.exe" codex-hook`
	changes, err := Init(p, command, true, time.Now())
	if err != nil || len(changes) != 2 {
		t.Fatalf("%v %v", changes, err)
	}
	b, _ := os.ReadFile(p.Hooks)
	if string(b) != original {
		t.Fatal("dry run wrote hooks")
	}
	if !strings.Contains(changes[0].Content, "other-tool") {
		t.Fatal("lost existing hook")
	}
	changes, err = Init(p, command, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	backup, _ := os.ReadFile(changes[0].Backup)
	if string(backup) != original {
		t.Fatal("bad backup")
	}
	changes, err = Init(p, command, false, time.Now())
	if err != nil || len(changes) != 0 {
		t.Fatalf("not idempotent: %+v %v", changes, err)
	}
	b, _ = os.ReadFile(p.AgentsMD)
	if !strings.HasPrefix(string(b), "# My instructions") || !strings.Contains(string(b), "trk codex start") {
		t.Fatal(string(b))
	}
}

func TestInvalidInputsWriteNothing(t *testing.T) {
	for _, raw := range []string{`{`, `{"hooks":[]}`, `{"hooks":{"Stop":{}}}`, `{"hooks":{"Stop":[{"hooks":[null]}]}}`, `{"hooks":{"Stop":[{"matcher":[],"hooks":[{"command":"trk codex-hook"}]}]}}`} {
		dir := t.TempDir()
		p := Paths{filepath.Join(dir, "hooks.json"), filepath.Join(dir, "AGENTS.md")}
		os.WriteFile(p.Hooks, []byte(raw), 0o600)
		if _, err := Init(p, "trk codex-hook", false, time.Now()); err == nil {
			t.Errorf("accepted %s", raw)
		}
		b, _ := os.ReadFile(p.Hooks)
		if string(b) != raw {
			t.Fatal("modified bad input")
		}
		if _, err := os.Stat(p.AgentsMD); !os.IsNotExist(err) {
			t.Fatal("created instructions after failure")
		}
	}
	dir := t.TempDir()
	p := Paths{filepath.Join(dir, "hooks.json"), filepath.Join(dir, "AGENTS.md")}
	os.WriteFile(p.AgentsMD, []byte(begin), 0o600)
	if _, err := Init(p, "trk codex-hook", false, time.Now()); err == nil {
		t.Fatal("accepted broken markers")
	}
	if _, err := os.Stat(p.Hooks); !os.IsNotExist(err) {
		t.Fatal("wrote hooks before validating instructions")
	}
}

func TestSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	link := filepath.Join(dir, "hooks.json")
	os.WriteFile(real, []byte(`{}`), 0o600)
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if _, err := Init(Paths{Hooks: link}, "trk codex-hook", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(link)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
}
