package codex

import (
	"encoding/json"
	"github.com/GujaLomsadze/trk/internal/agent"
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
		n, ok := Adapter{}.Normalize(raw)
		if !ok || n.Kind != want[i] || n.Native != "codex-fixture" || !json.Valid(n.Payload) {
			t.Fatalf("event %d: %s %s %t", i, n.Kind, n.Native, ok)
		}
		if n.Key != "" {
			if keys[n.Key] {
				t.Fatal("different stage/turn reused key")
			}
			keys[n.Key] = true
		}
		n2, _ := Adapter{}.Normalize(raw)
		if string(n.Payload) != string(n2.Payload) || n.Key != n2.Key {
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
		if _, ok := (Adapter{}).Normalize(json.RawMessage(raw)); ok {
			t.Errorf("accepted %s", raw)
		}
	}
	n, ok := Adapter{}.Normalize(json.RawMessage(`{"session_id":"s","hook_event_name":"Stop","rate_limits":{"five_hour":100},"cost":{"total_cost_usd":99}}`))
	if !ok || strings.Contains(string(n.Payload), "rate_limits") || strings.Contains(string(n.Payload), "cost") {
		t.Fatalf("%s", n.Payload)
	}
}

func TestCanonicalToolFields(t *testing.T) {
	n, _ := Adapter{}.Normalize(json.RawMessage(`{"session_id":"s","turn_id":"t1","hook_event_name":"PostToolUse","tool_name":"apply_patch","tool_use_id":"p1","tool_input":{"command":"*** Update File: a.go\n*** Add File: b.go"},"tool_response":{"exit_code":1}}`))
	var p struct {
		ToolUseID string   `json:"tool_use_id"`
		Files     []string `json:"tool_files"`
		Kind      string   `json:"tool_kind"`
		Outcome   string   `json:"tool_outcome"`
	}
	json.Unmarshal(n.Payload, &p)
	if n.Kind != "tool_fail" || p.ToolUseID != "t1/p1" || !reflect.DeepEqual(p.Files, []string{"a.go", "b.go"}) || p.Kind != "edit" || p.Outcome != "failed" {
		t.Fatalf("%s %+v", n.Kind, p)
	}
}

func TestBackgroundAndDefer(t *testing.T) {
	pre := suggestionPreamble
	for prompt, want := range map[string]bool{
		pre + " Return a suggestion.": true, "  " + pre: true, "Explain: " + pre: false,
		"Suggest my next message": false, `{"suggestion":"x"}`: false, "": false,
	} {
		b, _ := json.Marshal(map[string]string{"session_id": "s", "hook_event_name": "UserPromptSubmit", "prompt": prompt})
		if n, _ := (Adapter{}).Normalize(b); n.Background != want {
			t.Errorf("%q: %v", prompt, n.Background)
		}
	}
	n, _ := Adapter{}.Normalize(json.RawMessage(`{"session_id":"s","hook_event_name":"SessionStart","source":"fork"}`))
	m, _ := Adapter{}.Normalize(json.RawMessage(`{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`))
	if !n.Defer || m.Defer {
		t.Fatal("defer only forks")
	}
}

func TestHookReplyAndLaunch(t *testing.T) {
	if (Adapter{}).HookReply(json.RawMessage(`{"hook_event_name":"Stop"}`)) != "{}" || (Adapter{}).HookReply(json.RawMessage(`{"hook_event_name":"PreToolUse"}`)) != "" {
		t.Fatal("reply")
	}
	t.Setenv("TRK_CODEX_COMMAND", os.Args[0])
	l, err := Adapter{}.Launch(agent.LaunchSpec{Resume: "abc", Fork: true, Prompt: "-rf"})
	if err != nil || !reflect.DeepEqual(l.Args, []string{"--no-daemon", "fork", "abc", "--", "-rf"}) {
		t.Fatal(l, err)
	}
}
