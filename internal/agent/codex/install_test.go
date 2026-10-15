package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GujaLomsadze/trk/internal/agent"
)

func paths(t *testing.T) Paths {
	dir := t.TempDir()
	return Paths{filepath.Join(dir, "hooks.json"), filepath.Join(dir, "AGENTS.md")}
}

func TestInstallDryRunPreservesAndIsIdempotent(t *testing.T) {
	p := paths(t)
	original := `{"description":"mine","hooks":{"Stop":[{"hooks":[{"type":"command","command":"other-tool"}]}]}}`
	os.WriteFile(p.Hooks, []byte(original), 0o600)
	os.WriteFile(p.AgentsMD, []byte("# My instructions\n"), 0o600)
	cmd := `"/opt/my tools/trk" hook --agent codex`
	o := agent.InstallOptions{DryRun: true, Now: time.Now()}
	res, err := install(p, cmd, o)
	if err != nil || !res.Changed() || len(res.Steps) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(p.Hooks); string(b) != original {
		t.Fatal("dry run wrote hooks")
	}
	o.DryRun = false
	if res, err = install(p, cmd, o); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(res.Steps[0].Backup); string(b) != original {
		t.Fatal("bad backup")
	}
	b, _ := os.ReadFile(p.Hooks)
	for _, want := range []string{"other-tool", `"description": "mine"`, cmd[1:15], "PostCompact", `"async": false`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %q in %s", want, b)
		}
	}
	md, _ := os.ReadFile(p.AgentsMD)
	if !strings.HasPrefix(string(md), "# My instructions") || !strings.Contains(string(md), `trk step "`) {
		t.Fatal(string(md))
	}
	if res, err = install(p, cmd, o); err != nil || res.Changed() {
		t.Fatalf("not idempotent: %+v %v", res, err)
	}
}

func TestInstallUpdatesFirstReleaseHook(t *testing.T) {
	p := paths(t)
	os.WriteFile(p.Hooks, []byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/old/trk codex-hook","async":true,"timeout":3}]}]}}`), 0o600)
	if _, err := install(Paths{Hooks: p.Hooks}, "/new/trk hook --agent codex", agent.InstallOptions{Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p.Hooks)
	if strings.Contains(string(b), "codex-hook") || strings.Count(string(b), "/new/trk hook --agent codex") != len(Events) {
		t.Fatal(string(b))
	}
}

func TestInvalidInputsWriteNothing(t *testing.T) {
	for _, raw := range []string{`{`, `{"hooks":[]}`, `{"hooks":{"Stop":{}}}`, `{"hooks":{"Stop":[{"hooks":[null]}]}}`, `{"hooks":{"Stop":[{"matcher":[],"hooks":[{"command":"trk hook --agent codex"}]}]}}`} {
		p := paths(t)
		os.WriteFile(p.Hooks, []byte(raw), 0o600)
		if _, err := install(p, "trk hook --agent codex", agent.InstallOptions{Now: time.Now()}); err == nil {
			t.Errorf("accepted %s", raw)
		}
		if b, _ := os.ReadFile(p.Hooks); string(b) != raw {
			t.Fatal("modified bad input")
		}
		if _, err := os.Stat(p.AgentsMD); !os.IsNotExist(err) {
			t.Fatal("wrote AGENTS.md after failure")
		}
	}
}

func TestOwnCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/usr/local/bin/trk hook --agent codex": true, `"C:/Program Files/trk.exe" hook --agent codex`: true,
		"trk codex-hook": true, "trk hook": false, "mytrk hook --agent codex": false, "other-tool": false,
	} {
		if isOwnCommand(cmd) != want {
			t.Errorf("%q", cmd)
		}
	}
}
