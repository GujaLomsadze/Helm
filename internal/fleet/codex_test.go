package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/GujaLomsadze/helm/internal/derive"
	"github.com/GujaLomsadze/helm/internal/model"
)

func codexHook(sid, event, cwd, extra string) model.Envelope {
	e := hook(sid, event, cwd, extra)
	e.Agent = "codex"
	return e
}

func TestCodexFixtureLifecycle(t *testing.T) {
	b, err := os.ReadFile("../agent/codex/testdata/lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var raws []json.RawMessage
	json.Unmarshal(b, &raws)
	f := newFleet()
	now := 1000 * sec
	for i, raw := range raws {
		if _, ok := ingest(t, f, model.Envelope{Source: "hook", Agent: "codex", TS: now + int64(i)*sec, Payload: raw}, now+int64(i)*sec); !ok {
			t.Fatalf("event %d dropped", i)
		}
	}
	end := now + int64(len(raws))*sec
	sv, ok := f.SessionView("codex:codex-fixture", end)
	if !ok || sv.Agent != "codex" || sv.NativeID != "codex-fixture" {
		t.Fatalf("%+v", sv.Session)
	}
	if sv.Status != derive.StatusDone { // interrupted, then the session ended
		t.Fatalf("status %s", sv.Status)
	}
	if sv.FilesEdited != 4 || sv.Subagents["explorer"] != 1 || sv.LastReply == "" {
		t.Fatalf("%+v", sv)
	}
	for _, c := range sv.Recent {
		if c.State == "run" {
			t.Fatalf("call left running after the session ended: %+v", c)
		}
	}
	if f.Account().UpdatedAt != 0 {
		t.Fatal("codex touched the Claude account")
	}
}

func TestCodexAndClaudeShareANativeID(t *testing.T) {
	f := newFleet()
	now := 1000 * sec
	ingest(t, f, hook("same", "UserPromptSubmit", "/r/a", `,"prompt":"claude one"`), now)
	ingest(t, f, codexHook("same", "UserPromptSubmit", "/r/a", `,"prompt":"codex one"`), now+sec)
	if len(f.View(now+2*sec).Sessions) != 2 {
		t.Fatal("ids collided")
	}
	if s, _ := f.SessionRow("same"); s.Agent != "claude" || s.LastPrompt != "claude one" {
		t.Fatalf("%+v", s)
	}
}

func TestInterruptAndLateEvents(t *testing.T) {
	f := newFleet()
	now := 1000 * sec
	ingest(t, f, codexHook("x", "UserPromptSubmit", "/r/a", `,"prompt":"go","turn_id":"t1"`), now)
	ingest(t, f, codexHook("x", "PreToolUse", "/r/a", `,"tool_name":"Bash","tool_use_id":"c1","turn_id":"t1","tool_input":{"command":"sleep 9"}`), now+sec)
	ingest(t, f, codexHook("x", "Interrupt", "/r/a", `,"turn_id":"t1"`), now+2*sec)
	sv, _ := f.SessionView("codex:x", now+3*sec)
	if sv.Status != derive.StatusInterrupted || sv.Recent[0].State != "unknown" {
		t.Fatalf("%s %+v", sv.Status, sv.Recent)
	}
	// An async prompt older than the interrupt doesn't reopen the turn, but its text is kept.
	ingest(t, f, model.Envelope{Source: "hook", Agent: "codex", TS: now + sec/2, Payload: json.RawMessage(`{"session_id":"x","hook_event_name":"UserPromptSubmit","prompt":"older"}`)}, now+4*sec)
	if sv, _ = f.SessionView("codex:x", now+4*sec); sv.Status != derive.StatusInterrupted {
		t.Fatal(sv.Status)
	}
	// A new prompt does.
	ingest(t, f, codexHook("x", "UserPromptSubmit", "/r/a", `,"prompt":"again"`), now+5*sec)
	if sv, _ = f.SessionView("codex:x", now+5*sec); sv.Status != derive.StatusWorking || sv.LastPrompt != "again" {
		t.Fatal(sv.Status, sv.LastPrompt)
	}
}

func TestClaudeLatePromptDoesNotReopen(t *testing.T) {
	f := newFleet()
	now := 1000 * sec
	ingest(t, f, hook("A", "UserPromptSubmit", "/r/a", `,"prompt":"first"`), now)
	ingest(t, f, hook("A", "Stop", "/r/a", ""), now+5*sec)
	late := hook("A", "SessionStart", "/r/a", "")
	late.TS = now + sec
	ingest(t, f, late, now+6*sec)
	if sv, _ := f.SessionView("A", now+6*sec); sv.Status != derive.StatusDone {
		t.Fatal(sv.Status)
	}
}

func TestSuggestionForksStayHidden(t *testing.T) {
	f := newFleet()
	now := 1000 * sec
	ingest(t, f, codexHook("real", "UserPromptSubmit", "/r/a", `,"prompt":"fix it"`), now)
	ingest(t, f, codexHook("fork", "SessionStart", "/r/a", `,"source":"fork"`), now+sec)
	if n := len(f.View(now + 2*sec).Sessions); n != 1 {
		t.Fatalf("blank fork shown: %d cards", n)
	}
	ingest(t, f, codexHook("fork", "UserPromptSubmit", "/r/a", `,"prompt":"This is an app-generated request for a suggested next user message. The user did not write this message. Reply."`), now+2*sec)
	if n := len(f.View(now + 3*sec).Sessions); n != 1 {
		t.Fatalf("suggestion fork shown: %d cards", n)
	}
	if s, _ := f.SessionRow("codex:fork"); !s.Background {
		t.Fatal("not marked background")
	}
	// Progress from that folder goes to the one real chat, never the suggestion fork.
	ev, _ := ingest(t, f, cli("step", `{"text":"x","cwd":"/r/a"}`, 0), now+4*sec)
	if ev.SessionID != "codex:real" {
		t.Fatal(ev.SessionID)
	}
	// A real fork shows once it does something.
	ingest(t, f, codexHook("fork2", "SessionStart", "/r/a", `,"source":"fork"`), now+5*sec)
	ingest(t, f, codexHook("fork2", "UserPromptSubmit", "/r/a", `,"prompt":"try another way"`), now+6*sec)
	if n := len(f.View(now + 7*sec).Sessions); n != 2 {
		t.Fatalf("user fork hidden: %d cards", n)
	}
}

func TestCodexCwdAttributionNeedsOneLiveChat(t *testing.T) {
	f := newFleet()
	now := 1000 * sec
	ingest(t, f, codexHook("a", "UserPromptSubmit", "/r/a", `,"prompt":"1"`), now)
	ingest(t, f, codexHook("b", "UserPromptSubmit", "/r/a", `,"prompt":"2"`), now+sec)
	ev, _ := ingest(t, f, cli("step", `{"text":"x","cwd":"/r/a"}`, 0), now+2*sec)
	if ev.SessionID == "codex:a" || ev.SessionID == "codex:b" {
		t.Fatalf("guessed between two chats: %s", ev.SessionID)
	}
	ev, _ = ingest(t, f, model.Envelope{Source: "cli", Kind: "step", SessionID: "codex:b", Payload: json.RawMessage(`{"text":"y"}`)}, now+3*sec)
	if ev.SessionID != "codex:b" || ev.Attribution != model.AttrNative || ev.Agent != "" {
		t.Fatalf("%+v", ev)
	}
	if s, _ := f.SessionRow("codex:b"); s.Agent != "codex" || s.StepText != "y" {
		t.Fatalf("progress changed the session's agent: %+v", s)
	}
}

func TestCodexPatchCollidesWithClaudeEdit(t *testing.T) {
	f := newFleet()
	now := 1000 * sec
	ingest(t, f, hook("A", "PreToolUse", "/r/a", `,"tool_name":"Edit","tool_use_id":"e1","tool_input":{"file_path":"/r/a/main.go"}`), now)
	patch := fmt.Sprintf(`,"tool_name":"apply_patch","tool_use_id":"p1","turn_id":"t","tool_input":{"command":%q}`, "*** Update File: main.go\n*** Add File: new.go")
	ingest(t, f, codexHook("x", "PreToolUse", "/r/a", patch), now+sec)
	v := f.View(now + 2*sec)
	if len(v.Collisions) != 1 || v.Collisions[0].Path != "/r/a/main.go" {
		t.Fatalf("%+v", v.Collisions)
	}
}
