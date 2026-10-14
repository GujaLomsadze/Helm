package daemon

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/GujaLomsadze/trk/internal/fleet"
	"github.com/GujaLomsadze/trk/internal/gitinfo"
	"github.com/GujaLomsadze/trk/internal/model"
	"github.com/GujaLomsadze/trk/internal/stats"
)

func TestBackgroundSuggestionsExcludedFromDashboardAndStats(t *testing.T) {
	ts, srv := newTestServer(t)
	base := time.Now().UnixMilli() - 10000
	for i, sid := range []string{"main", "suggestion"} {
		prompt := "Build the app"
		if i == 1 {
			prompt = "This is an app-generated request for a suggested next user message. The user did not write this message. Return JSON."
		}
		for j, event := range []string{"UserPromptSubmit", "Stop", "SessionEnd"} {
			payload, _ := json.Marshal(map[string]any{"session_id": sid, "hook_event_name": event, "cwd": "/repo", "prompt": prompt})
			body, _ := json.Marshal(model.Envelope{Provider: "codex", Source: "hook", TS: base + int64(i*10+j), Payload: payload})
			if code := post(t, ts.URL, string(body), nil); code != http.StatusAccepted {
				t.Fatal(code)
			}
		}
	}
	var view fleet.View
	getJSON(t, ts.URL+"/v1/sessions", &view)
	if len(view.Sessions) != 1 || view.Sessions[0].SessionID != "codex:main" {
		t.Fatalf("%+v", view)
	}
	var result stats.Result
	getJSON(t, ts.URL+"/v1/stats?days=1&provider=codex", &result)
	if len(result.Sessions) != 1 || result.Sessions[0].ID != "codex:main" || result.KPI.UnknownCostSessions != 1 {
		t.Fatalf("%+v", result)
	}
	rows, _ := srv.st.Sessions()
	events, _ := srv.st.SessionEvents("codex:suggestion", 100)
	if len(rows) != 2 || len(events) != 3 {
		t.Fatal("history was deleted")
	}
	restarted := fleet.New(gitinfo.Lookup)
	if err := restore(srv.st, restarted, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if got := restarted.View(time.Now().UnixMilli()); len(got.Sessions) != 1 {
		t.Fatalf("replay: %+v", got)
	}
}
