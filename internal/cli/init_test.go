package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/GujaLomsadze/trk/internal/agent"
)

func TestIsTTYRejectsDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	if isTTY(f) {
		t.Fatal("/dev/null treated as an interactive terminal")
	}
}

func TestRenderInitReport(t *testing.T) {
	home, _ := os.UserHomeDir()
	settings, md := home+"/.claude/settings.json", home+"/.claude/CLAUDE.md"
	r := agent.InstallResult{Steps: []agent.InstallStep{
		{Label: "Hooks", State: agent.StepAdded, Detail: settings, Backup: settings + ".trk-backup-1"},
		{Label: "Status line", State: agent.StepAdded, Detail: "chained — ccstatusline still shows, unchanged", Backup: settings + ".trk-backup-1"},
		{Label: "CLAUDE.md", State: agent.StepAdded, Detail: md, Backup: md + ".trk-backup-1"},
	}, Next: []string{"Restart your Claude Code sessions (hooks load at session start)"}}
	var b bytes.Buffer
	renderInit(&b, ui{}, "Claude Code", r, false, true, 7777)
	out := b.String()
	for _, want := range []string{"connected to Claude Code", "Hooks", "~/.claude/settings.json", "ccstatusline", "~/.claude/settings.json.trk-backup-1",
		"Next", "Restart", "trk open", "http://localhost:7777"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "settings.json.trk-backup-1") != 1 {
		t.Errorf("backup listed twice:\n%s", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("ANSI codes with color off")
	}
	b.Reset()
	renderInit(&b, ui{}, "Codex", r, true, false, 7777)
	if !strings.Contains(b.String(), "dry run") || strings.Contains(b.String(), "Restart") || !strings.Contains(b.String(), "trk init --agent codex") {
		t.Errorf("dry-run report:\n%s", b.String())
	}
}

func TestInitCodexDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	code, out, stderr := run("init", "--agent", "codex", "--dry-run")
	if code != 0 || stderr != "" || !strings.Contains(out, "Hooks") || !strings.Contains(out, "AGENTS.md") {
		t.Fatalf("%d %s %s", code, out, stderr)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("dry run wrote files")
	}
	if code, _, _ := run("init", "--agent", "nope"); code != 2 {
		t.Fatal("unknown agent accepted")
	}
}
