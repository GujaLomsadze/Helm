package stats

import (
	"github.com/GujaLomsadze/trk/internal/store"
	"testing"
)

func TestProviderFilterAndSharedToolIDs(t *testing.T) {
	rows := []store.StatRow{
		{Session: "claude", TS: 1, Kind: "tool_pre", ToolUseID: "same", Tool: "Bash"},
		{Session: "codex:s", TS: 2, Kind: "tool_pre", ToolUseID: "same", Tool: "apply_patch", Files: []string{"a.go", "b.go"}},
		{Session: "claude", TS: 3, Kind: "tool_post", ToolUseID: "same", Tool: "Bash"},
		{Session: "codex:s", TS: 4, Kind: "tool_post", ToolUseID: "same", Tool: "apply_patch"},
		{Session: "codex:s", TS: 5, Kind: "status", HasStatus: true, HasTokens: true, Tokens: 150},
	}
	in := Input{Rows: rows, Days: 1, Since: 0, Until: 10, Meta: map[string]Meta{"claude": {Provider: "claude"}, "codex:s": {Provider: "codex", Cwd: "/r"}}, Prior: map[string]store.StatRow{"codex:s": {HasStatus: true, HasTokens: true, Tokens: 100}}}
	r := Compute(in)
	if r.KPI.Tokens != 50 || r.KPI.UnknownCostSessions != 2 || r.KPI.KnownCostSessions != 0 || len(r.Files) != 2 {
		t.Fatalf("%+v", r)
	}
	for _, s := range r.Sessions {
		if s.TimeKnown || s.CostKnown {
			t.Fatalf("invented usage: %+v", s)
		}
		if s.ToolMs != 2 {
			t.Fatalf("tool IDs crossed sessions: %+v", s)
		}
	}
	in.Provider = "codex"
	r = Compute(in)
	if len(r.Sessions) != 1 || r.Sessions[0].Provider != "codex" || r.KPI.ToolCalls != 1 {
		t.Fatalf("%+v", r)
	}
}
