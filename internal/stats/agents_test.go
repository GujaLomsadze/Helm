package stats

import (
	"testing"

	"github.com/GujaLomsadze/trk/internal/store"
)

func TestAgentFilterBackgroundAndPatches(t *testing.T) {
	rows := []store.StatRow{
		{TS: 100, Session: "c", Kind: "status", Cost: 1, Tokens: 10, HasStatus: true},
		{TS: 110, Session: "c", Kind: "tool_pre", Tool: "Edit", ToolUseID: "1", File: "/r/a.go"},
		{TS: 120, Session: "x", Kind: "tool_pre", Tool: "apply_patch", ToolUseID: "1", ToolKind: "edit", Files: []string{"a.go", "b.go"}},
		{TS: 130, Session: "c", Kind: "tool_post", Tool: "Edit", ToolUseID: "1"}, // same id, other session
		{TS: 140, Session: "bg", Kind: "tool_pre", Tool: "Bash", ToolUseID: "9", Command: "ls"},
	}
	meta := map[string]Meta{
		"c":  {Cwd: "/r", Repo: "r", Agent: "claude"},
		"x":  {Cwd: "/r", Repo: "r", Agent: "codex"},
		"bg": {Cwd: "/r", Repo: "r", Agent: "codex", Background: true},
	}
	in := Input{Rows: rows, Meta: meta, Since: 0, Until: 1000, Days: 1}
	all := Compute(in)
	if all.KPI.ToolCalls != 2 {
		t.Fatalf("background work counted: %d calls", all.KPI.ToolCalls)
	}
	edits := map[string]int{}
	for _, f := range all.Files {
		edits[f.Path] = f.Edits
	}
	if edits["a.go"] != 2 || edits["b.go"] != 1 {
		t.Fatalf("%+v", all.Files)
	}
	in.Agent = "codex"
	cx := Compute(in)
	if cx.KPI.ToolCalls != 1 || cx.KPI.Spend != 0 || len(cx.Repos) != 0 {
		t.Fatalf("%+v", cx.KPI)
	}
	in.Agent = "claude"
	if cl := Compute(in); cl.KPI.ToolCalls != 1 || cl.KPI.Spend != 1 || cl.Sessions[0].ToolMs != 20 {
		t.Fatalf("%+v %+v", cl.KPI, cl.Sessions)
	}
}
