package codex

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestLifecycleFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var events []json.RawMessage
	if err := json.Unmarshal(b, &events); err != nil {
		t.Fatal(err)
	}
	want := []string{"session_start", "prompt", "tool_pre", "permission", "tool_fail", "tool_pre", "tool_post", "compact", "compacted", "subagent_start", "subagent_stop", "stop", "session_start", "prompt", "tool_pre", "interrupt", "session_end"}
	keys := map[string]bool{}
	for i, raw := range events {
		p, kind, sid, key, ok := Normalize(raw)
		if !ok || kind != want[i] || sid != "codex-fixture" || !json.Valid(p) {
			t.Fatalf("event %d: %s %s %t", i, kind, sid, ok)
		}
		if key != "" {
			if keys[key] {
				t.Fatal("different stage/turn reused key")
			}
			keys[key] = true
		}
		p2, _, _, key2, _ := Normalize(raw)
		if string(p) != string(p2) || key != key2 {
			t.Fatal("not deterministic")
		}
	}
}

func TestOutcome(t *testing.T) {
	for _, c := range []struct{ raw, tool, want string }{
		{`{"exit_code":0}`, "Bash", "ok"}, {`{"exit_code":2}`, "Bash", "failed"},
		{`{"isError":true}`, "mcp__x", "failed"}, {`{"isError":false}`, "mcp__x", "ok"},
		{`"hello error failure"`, "Bash", "unknown"}, {`null`, "Bash", "unknown"},
		{`"Process exited with code 0\nFinal output:\nok"`, "Bash", "ok"},
		{`"Exit code: 9\nOutput:\nfailed"`, "Bash", "failed"},
		{`"Process exited with code 0\nFinal output:\nProcess exited with code 99"`, "Bash", "ok"},
		{`"Output:\nExit code: 2"`, "Bash", "unknown"},
		{`"Process exited with code 1"`, "mcp__x", "unknown"},
	} {
		if got := Outcome(json.RawMessage(c.raw), c.tool); got != c.want {
			t.Errorf("%s: %s", c.raw, got)
		}
	}
}

func TestPatchFilesAndInvalidPayloads(t *testing.T) {
	want := []string{"a b.go", "next.go", "old.go", "new.go"}
	got := PatchFiles("*** Update File: a b.go\n*** Move to: next.go\n*** Delete File: old.go\n*** Add File: new.go\n*** Update File: a b.go\n+*** Add File: fake.go")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	for _, raw := range []string{"", "null", "[]", `{"session_id":42}`, `{}`} {
		if _, _, _, _, ok := Normalize(json.RawMessage(raw)); ok {
			t.Errorf("accepted %s", raw)
		}
	}
	p, _, _, _, ok := Normalize(json.RawMessage(`{"session_id":"s","hook_event_name":"Stop","rate_limits":{"five_hour":100},"cost":{"total_cost_usd":99}}`))
	if !ok || strings.Contains(string(p), "rate_limits") || strings.Contains(string(p), "cost") {
		t.Fatalf("%s", p)
	}
}
