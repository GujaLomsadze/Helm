package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GujaLomsadze/trk/internal/agent"
	"github.com/GujaLomsadze/trk/internal/agent/claude"
)

// limitsAgent is a test adapter that can read plan usage.
type limitsAgent struct {
	claude.Adapter
	reads atomic.Int32
	fail  atomic.Bool
	gate  chan struct{}
}

func (*limitsAgent) Name() string  { return "limitstest" }
func (*limitsAgent) Label() string { return "Limits Test" }
func (a *limitsAgent) Limits(ctx context.Context) ([]agent.LimitWindow, error) {
	a.reads.Add(1)
	if a.gate != nil {
		<-a.gate
	}
	if a.fail.Load() {
		return nil, errors.New("not signed in")
	}
	v := 42.0
	return []agent.LimitWindow{{ID: "w", Label: "5-hour", DurationMin: 300, UsedPct: &v}}, nil
}

var testAgent = &limitsAgent{}

func init() { agent.Register(testAgent) }

func TestCodexHookDedupeAndAgent(t *testing.T) {
	ts, srv := newTestServer(t)
	ev := `{"source":"hook","agent":"codex","payload":{"session_id":"n1","turn_id":"t","hook_event_name":"PreToolUse","cwd":"/tmp/p","tool_name":"Bash","tool_use_id":"c","tool_input":{"command":"ls"}}}`
	if c := post(t, ts.URL, ev, nil); c != http.StatusAccepted {
		t.Fatal(c)
	}
	if c := post(t, ts.URL, ev, nil); c != http.StatusNoContent {
		t.Fatalf("redelivery stored again: %d", c)
	}
	sv, ok := srv.fl.SessionView("codex:n1", srv.now())
	if !ok || sv.Agent != "codex" || sv.ToolCalls != 1 {
		t.Fatalf("%+v", sv)
	}
	if c := post(t, ts.URL, `{"source":"hook","agent":"nope","payload":{"session_id":"x"}}`, nil); c != http.StatusNoContent {
		t.Fatalf("unknown agent: %d", c)
	}
}

func TestAgentsEndpoint(t *testing.T) {
	ts, srv := newTestServer(t)
	post(t, ts.URL, `{"source":"hook","agent":"codex","payload":{"session_id":"n1","hook_event_name":"UserPromptSubmit","cwd":"/tmp/p","prompt":"hi"}}`, nil)
	srv.terms.Command = "cat"
	var out []struct {
		Name      string
		Seen      bool
		Available bool
		Limits    bool
		Resume    string
	}
	getJSON(t, ts.URL+"/v1/agents", &out)
	got := map[string]bool{}
	for _, a := range out {
		got[a.Name] = true
		switch a.Name {
		case "codex":
			if !a.Seen || !a.Limits || !a.Available || a.Resume != "codex resume {id}" {
				t.Fatalf("%+v", a)
			}
		case "claude":
			if a.Seen || a.Limits || a.Resume != "claude --resume {id}" {
				t.Fatalf("%+v", a)
			}
		}
	}
	if !got["claude"] || !got["codex"] || out[0].Name != "claude" {
		t.Fatalf("%+v", out)
	}
}

func TestAgentLimitsShareOneRead(t *testing.T) {
	ts, srv := newTestServer(t)
	var now atomic.Int64
	now.Store(1_000_000)
	srv.now = func() int64 { return now.Load() }
	testAgent.reads.Store(0)
	testAgent.fail.Store(false)
	testAgent.gate = make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v limitsView
			getJSON(t, ts.URL+"/v1/agents/limitstest/limits", &v)
			if len(v.Windows) != 1 || *v.Windows[0].UsedPct != 42 {
				t.Errorf("%+v", v)
			}
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(testAgent.gate)
	wg.Wait()
	testAgent.gate = nil
	if n := testAgent.reads.Load(); n != 1 {
		t.Fatalf("%d reads for 5 viewers", n)
	}
	// Within a minute: cached. After: read again; a failure keeps the old reading, marked stale.
	var v limitsView
	getJSON(t, ts.URL+"/v1/agents/limitstest/limits", &v)
	testAgent.fail.Store(true)
	now.Add(limitsTTL)
	getJSON(t, ts.URL+"/v1/agents/limitstest/limits", &v)
	if testAgent.reads.Load() != 2 || !v.Stale || v.Error == "" || len(v.Windows) != 1 {
		t.Fatalf("%d %+v", testAgent.reads.Load(), v)
	}
	if r, _ := http.Get(ts.URL + "/v1/agents/claude/limits"); r.StatusCode != http.StatusNotFound {
		t.Fatal(r.StatusCode)
	}
}

func TestStatsAgentFilter(t *testing.T) {
	ts, _ := newTestServer(t)
	if r, _ := http.Get(ts.URL + "/v1/stats?agent=nope"); r.StatusCode != http.StatusBadRequest {
		t.Fatal(r.StatusCode)
	}
	var res json.RawMessage
	getJSON(t, ts.URL+"/v1/stats?agent=codex", &res)
}
