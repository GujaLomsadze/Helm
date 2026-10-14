package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/GujaLomsadze/trk/internal/fleet"
	"github.com/GujaLomsadze/trk/internal/gitinfo"
	"github.com/GujaLomsadze/trk/internal/model"
	"github.com/GujaLomsadze/trk/internal/stats"
)

// A complete mixed-provider HTTP -> SQLite -> view/stats -> restart replay test.
// Fixtures are synthetic documented payloads, not a recording of private conversations.
func TestCodexMixedFleetReplay(t *testing.T) {
	ts, srv := newTestServer(t)
	b, err := os.ReadFile("../codex/testdata/lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var payloads []json.RawMessage
	if err = json.Unmarshal(b, &payloads); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UnixMilli() - 20000
	for i, p := range payloads {
		env := model.Envelope{Provider: "codex", Source: "hook", TS: base + int64(i)*100, Payload: p}
		body, _ := json.Marshal(env)
		if code := post(t, ts.URL, string(body), nil); code != http.StatusAccepted {
			t.Fatalf("event %d: %d", i, code)
		}
		if i == 4 {
			if code := post(t, ts.URL, string(body), nil); code != http.StatusNoContent {
				t.Fatalf("duplicate accepted: %d", code)
			}
		}
	}
	if code := post(t, ts.URL, `{"source":"hook","payload":{"session_id":"codex-fixture","hook_event_name":"SessionStart","cwd":"/repo"}}`, nil); code != http.StatusAccepted {
		t.Fatal(code)
	}
	var v fleet.View
	getJSON(t, ts.URL+"/v1/sessions", &v)
	if len(v.Sessions) != 2 {
		t.Fatalf("mixed sessions: %+v", v.Sessions)
	}
	s, ok := srv.fl.SessionView("codex:codex-fixture", time.Now().UnixMilli())
	if !ok || s.Status != "done" || s.FilesEdited != 4 || s.ToolCalls != 3 || s.CostKnown || s.TokensKnown || s.LastReply != "Updated the tests" {
		t.Fatalf("view: %+v", s)
	}
	var history stats.Result
	getJSON(t, ts.URL+"/v1/stats?provider=codex&days=1", &history)
	if len(history.Sessions) != 1 || history.KPI.ToolCalls != 3 || history.KPI.ToolFails != 1 || len(history.Files) != 4 || history.KPI.UnknownCostSessions != 1 {
		t.Fatalf("stats: %+v", history)
	}
	fl := fleet.New(gitinfo.Lookup)
	if err := restore(srv.st, fl, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	r, _ := fl.SessionView("codex:codex-fixture", time.Now().UnixMilli())
	if r.Provider != "codex" || r.NativeID != "codex-fixture" || r.ToolCalls != s.ToolCalls || r.Subagents["explorer"] != 1 || r.Status != s.Status {
		t.Fatalf("restored %+v", r)
	}
	// Native account fields slipped into Codex input must never contaminate Claude limits.
	post(t, ts.URL, `{"source":"statusline","payload":{"session_id":"claude","rate_limits":{"five_hour":{"used_percentage":12}}}}`, nil)
	post(t, ts.URL, `{"provider":"codex","source":"hook","payload":{"session_id":"s","hook_event_name":"Stop","rate_limits":{"five_hour":{"used_percentage":99}}}}`, nil)
	if a := srv.fl.Account(); a.FiveHPct == nil || *a.FiveHPct != 12 {
		t.Fatalf("account: %+v", a)
	}
}
